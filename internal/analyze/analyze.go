package analyze

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"github.com/liustack/tifa/internal/config"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liustack/tifa/internal/langs"
	"golang.org/x/mod/module"
	"golang.org/x/tools/go/packages"
)

const (
	tifaPath = "github.com/liustack/tifa"
	corePath = "github.com/liustack/tifa/internal/core"
	ctxType  = "*" + corePath + ".Ctx"
)

// methods lists the HTTP methods whose handlers route.go may declare, each as a function named
// after it, in manifest order.
var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// hooks lists the hooks of hooks.go with their parameter and result types, in manifest order.
var hooks = []struct {
	name            string
	params, results []string
	signature       string
}{
	{"OnStart", []string{"context.Context"}, []string{"error"}, "func(ctx context.Context) error"},
	{"OnShutdown", []string{"context.Context"}, []string{"error"}, "func(ctx context.Context) error"},
	{"OnRequest", []string{ctxType, "func(" + ctxType + ") error"}, []string{"error"}, "func(c *tifa.Ctx, next func(*tifa.Ctx) error) error"},
	{"OnError", []string{ctxType, "error"}, []string{corePath + ".Error"}, "func(c *tifa.Ctx, err error) tifa.Error"},
}

// Analyze loads the application whose module root is dir and returns its route manifest.
// Convention violations come back as Errors, holding every violation found, sorted by position.
// Packages that do not type check, anywhere in the import graph, are reported with their errors.
// The app packages among them and those importing them, directly or through packages outside
// app/, are skipped. The conventions of all other packages are still checked.
// Other errors (no app directory, go list failing) are returned as they are.
func Analyze(dir string) (*Manifest, error) { return AnalyzeWith(dir, Options{}) }

// Options change what Analyze keeps.
type Options struct {
	// Drafts keeps the content entries whose front matter has draft: true, as tifa dev shows them.
	Drafts bool
}

// AnalyzeWith is Analyze with opts.
func AnalyzeWith(dir string, opts Options) (*Manifest, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	a := &analyzer{root: root, opts: opts, dirs: map[string]*dirInfo{}, identity: map[*Load][]identityRead{}, queryAt: map[*Load][]queryAt{}}
	if a.module, err = modulePath(root); err != nil {
		return nil, err
	}
	if err := a.scan(); err != nil {
		return nil, err
	}
	if err := a.load(); err != nil {
		return nil, err
	}
	m := a.build()
	if len(a.errs) > 0 {
		return nil, a.sortedErrors()
	}
	return m, nil
}

// modulePath returns the path of the module rooted at root, as the go command that loads and
// builds the application sees it, GOFLAGS and go.work included. An app whose pages are all TSX
// has no Go package to read it from.
func modulePath(root string) (string, error) {
	cmd := exec.Command("go", "list", "-m", "-json")
	cmd.Dir, cmd.Env = root, append(os.Environ(), "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -m in %s: %v\n%s", root, err, strings.TrimSpace(stderr.String()))
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	// In a workspace the go command lists every module of go.work.
	for dec := jsontext.NewDecoder(bytes.NewReader(out)); ; {
		var m struct{ Path, Dir string }
		if err := json.UnmarshalDecode(dec, &m); err == io.EOF {
			break
		} else if err != nil {
			return "", fmt.Errorf("go list -m in %s: %w", root, err)
		}
		if info, err := os.Stat(m.Dir); err == nil && os.SameFile(info, rootInfo) {
			return m.Path, nil
		}
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return "", fmt.Errorf("the go.work in use leaves out the module at %s: add it with go work use, or set GOWORK=off", root)
	}
	return "", fmt.Errorf("%s is not the root of a module: the application root holds go.mod", root)
}

type analyzer struct {
	root           string
	opts           Options
	module         string
	fset           *token.FileSet
	dirs           map[string]*dirInfo // by ID
	pkgs           []*packages.Package // app packages and the root package
	byDir          map[string]*packages.Package
	rootPk         *packages.Package   // nil without hooks.go
	embed          *Embedded           // what the packages of the module embed with go:embed
	modulePackages []*packages.Package // every package of the module that type checked, tests left out
	broken         map[string]bool     // directory IDs of packages skipped because they or their imports do not type check
	// locales holds the languages of each area, by the folder of its _locales folder. site is the languages of the site once i18n has worked them out.
	locales map[string][]string
	site    []string
	// appPkgs holds the import paths of pkgs, modulePkgs those of every package in the module.
	appPkgs, modulePkgs map[string]bool
	funcs               map[string]funcSource // functions of module packages that type check, by full name
	helperUses          []useCall
	errs                Errors
	identity            map[*Load][]identityRead // the reads of cookies, headers and the session by each Load
	queryAt             map[*Load][]queryAt      // the fields of the query struct of each Load, with where they are declared
}

// dirInfo records the convention files found in one directory under app/. replaceTSX is a
// layout_.tsx, a Layout shown without the Layouts above it.
type dirInfo struct {
	id                                      string
	routeGo, pageTSX, layoutTSX, replaceTSX bool
}

// replaces reports whether the Layout of d drops the Layouts above it. A layout_.tsx in app/, or
// next to a layout.tsx, is reported by scan and taken as a layout.tsx.
func (d *dirInfo) replaces() bool { return d.replaceTSX && !d.layoutTSX && d.id != "app" }

// conventionFiles are the files that make a directory under app/ a page, a Layout or an API
// route.
var conventionFiles = []string{"route.go", "page.tsx", "layout.tsx", "layout_.tsx"}

// scan walks app/ and records which convention files exist. The go command skips directories
// whose name starts with "." or "_" and directories named testdata, so a convention file below
// one of them is reported instead of silently left out.
func (a *analyzer) scan() error {
	appDir := filepath.Join(a.root, "app")
	if info, err := os.Stat(appDir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s has no app directory", a.root)
	}
	err := filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && name == langs.Dir {
			parent, err := filepath.Rel(appDir, filepath.Dir(path))
			if err != nil {
				return err
			}
			// The folders the go command skips hold no area, as for tifa.Languages.
			if !langs.Hidden(filepath.ToSlash(parent)) {
				if err := a.readLocales(path, a.id(path)); err != nil {
					return err
				}
			}
			return filepath.SkipDir
		}
		if d.IsDir() || !slices.Contains(conventionFiles, name) {
			return nil
		}
		id := a.id(filepath.Dir(path))
		if reservedDir(id) {
			a.errorInFile(id, name, "the path /__tifa/ is reserved for tifa itself, such as its dev server: rename the directory")
			return nil
		}
		if skipped := skippedDir(id); skipped != "" {
			a.errorInFile(id, name, "the go command skips %s because its name starts with . or _ or is testdata, so this file is never built: rename the directory", skipped)
			return nil
		}
		info := a.dirs[id]
		if info == nil {
			info = &dirInfo{id: id}
			a.dirs[id] = info
		}
		switch name {
		case "route.go":
			info.routeGo = true
		case "page.tsx":
			info.pageTSX = true
		case "layout.tsx":
			info.layoutTSX = true
		case "layout_.tsx":
			info.replaceTSX = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, d := range a.dirs {
		switch {
		case !d.replaceTSX:
		case d.id == "app":
			a.errorInFile(d.id, "layout_.tsx", "layout_.tsx drops the layouts above it, and app/ has none: rename it to layout.tsx")
		case d.layoutTSX:
			a.errorInFile(d.id, "layout_.tsx", "layout.tsx and layout_.tsx are both in %s, and a folder has one layout: keep layout.tsx to show it inside the layouts above, or layout_.tsx to show it without them", d.id)
		}
	}
	return nil
}

// reservedDir reports whether the directory ID id, below app, is the URL /__tifa or below it, which
// the framework answers itself.
func reservedDir(id string) bool {
	parts := strings.Split(id, "/")
	return len(parts) > 1 && parts[1] == "__tifa"
}

// skippedDir returns the outermost directory of the directory ID id, below app, that the go
// command skips, or "" when it builds id.
func skippedDir(id string) string {
	parts := strings.Split(id, "/")
	for i, name := range parts[1:] {
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" {
			return strings.Join(parts[:i+2], "/")
		}
	}
	return ""
}

// id turns an absolute directory into its ID: the path relative to the application root with forward slashes.
func (a *analyzer) id(dir string) string {
	rel, err := filepath.Rel(a.root, dir)
	if err != nil {
		panic(err) // every directory comes from walking or loading below the root
	}
	return filepath.ToSlash(rel)
}

// load type checks every package of the module from source, .tifa/ and other directories the
// go command skips excluded. Calls that pass the Ctx out of a Load are followed into helpers in
// any of them, so packages outside app/, such as internal/, need their syntax too.
func (a *analyzer) load() error {
	hasHooks, hasConfig := false, false
	if _, err := os.Stat(filepath.Join(a.root, "hooks.go")); err == nil {
		hasHooks = true
	}
	if _, err := os.Stat(filepath.Join(a.root, "options.go")); err == nil {
		hasConfig = true
	}
	// content.go declares the content collections, which the generated main imports too.
	if _, err := os.Stat(filepath.Join(a.root, "content.go")); err == nil {
		hasConfig = true
	}
	a.fset = token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule | packages.NeedEmbedFiles | packages.NeedEmbedPatterns,
		Dir:  a.root,
		Fset: a.fset,
		// The application is built with cgo off, so the same files are analyzed.
		Env: append(os.Environ(), "CGO_ENABLED=0"),
	}
	patterns := []string{"./..."}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		// packages.Load hides some module-wide failures of go list, such as a go.mod that needs
		// updating, and returns no packages. Ask the go command for its own message.
		cmd := exec.Command("go", append([]string{"list", "-e"}, patterns...)...)
		cmd.Dir, cmd.Env = a.root, cfg.Env
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go list in %s: %v\n%s", a.root, err, strings.TrimSpace(string(out)))
		}
	}
	// The application is the packages under app/ and, with hooks.go or options.go, the root
	// package, which the generated main imports. Other packages of the module matter only
	// through what the application imports.
	var roots []*packages.Package
	a.modulePkgs = map[string]bool{}
	for _, p := range pkgs {
		if len(p.GoFiles) == 0 {
			continue
		}
		a.modulePkgs[p.PkgPath] = true
		if len(p.Errors) == 0 && !p.IllTyped && p.TypesInfo != nil {
			a.modulePackages = append(a.modulePackages, p)
		}
		id := a.id(filepath.Dir(p.GoFiles[0]))
		if id == "app" || strings.HasPrefix(id, "app/") || id == "." && (hasHooks || hasConfig) {
			roots = append(roots, p)
		}
	}
	a.embed = a.embedded(pkgs)
	a.byDir = map[string]*packages.Package{}
	a.broken = map[string]bool{}
	a.appPkgs = map[string]bool{}
	a.funcs = map[string]funcSource{}
	// Walk the import graph of the application, packages outside app/ included. A package with
	// errors is broken, and so is every package that imports a broken one directly or
	// indirectly, because it was type checked against incomplete information. Visit calls post
	// after the imports of a package, so imports are decided first.
	brokenPaths := map[string]bool{}
	packages.Visit(roots, nil, func(p *packages.Package) {
		if len(p.Errors) > 0 || p.IllTyped {
			brokenPaths[p.PkgPath] = true
		}
		for path := range p.Imports {
			if brokenPaths[path] {
				brokenPaths[p.PkgPath] = true
			}
		}
		// go list compiles each package for export data and reports the compiler output as one
		// error without a position. The type checker reports the same problems with positions.
		positioned := slices.ContainsFunc(p.Errors, func(e packages.Error) bool { return hasPos(e) })
		for _, e := range p.Errors {
			if hasPos(e) || !positioned {
				a.errs = append(a.errs, a.packageError(e))
			}
		}
		if a.modulePkgs[p.PkgPath] && !brokenPaths[p.PkgPath] {
			a.indexFuncs(p)
		}
	})
	if len(brokenPaths) > 0 && len(a.errs) == 0 {
		for path := range brokenPaths { // only reachable if go/packages flags a package without saying why
			a.errs = append(a.errs, Error{Msg: fmt.Sprintf("package %s does not type check", path)})
		}
	}
	for _, p := range roots {
		id := a.id(filepath.Dir(p.GoFiles[0]))
		if brokenPaths[p.PkgPath] {
			a.broken[id] = true
			continue
		}
		a.pkgs = append(a.pkgs, p)
		a.appPkgs[p.PkgPath] = true
		a.byDir[id] = p
		if id == "." && hasHooks {
			a.rootPk = p
		}
	}
	return nil
}

// funcSource is the declaration of a function or method in a module package that type checks.
type funcSource struct {
	pkg  *packages.Package
	decl *ast.FuncDecl
}

// indexFuncs records the declarations of a package's functions and methods by full name.
// Generic functions and methods are recorded once, under their generic name.
func (a *analyzer) indexFuncs(p *packages.Package) {
	for _, f := range p.Syntax {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fn, ok := p.TypesInfo.Defs[fd.Name].(*types.Func); ok {
					a.funcs[fn.FullName()] = funcSource{pkg: p, decl: fd}
				}
			}
		}
	}
}

func hasPos(e packages.Error) bool { return e.Pos != "" && e.Pos != "-" }

// packageError converts a load or type error, whose position is a "file:line:col" string.
func (a *analyzer) packageError(e packages.Error) Error {
	e.Msg = explainUse(a.explainCycle(e.Msg))
	if !hasPos(e) {
		return Error{Msg: e.Msg}
	}
	pos := token.Position{Filename: e.Pos}
	parts := strings.Split(e.Pos, ":")
	if len(parts) >= 3 {
		pos.Filename = strings.Join(parts[:len(parts)-2], ":")
		fmt.Sscan(parts[len(parts)-2], &pos.Line)
		fmt.Sscan(parts[len(parts)-1], &pos.Column)
	} else if len(parts) == 2 {
		pos.Filename = parts[0]
		fmt.Sscan(parts[1], &pos.Line)
	}
	pos.Filename = a.relFile(pos.Filename)
	return Error{Pos: pos, Msg: e.Msg}
}

// layoutWithQueryRe finds a function with a second parameter after the Ctx in the message the type
// checker gives for tifa.Use called with it.
var layoutWithQueryRe = regexp.MustCompile(`func\([^()]*\*tifa\.Ctx, [^()]*\)`)

// explainUse says what a type error of a tifa.Use call means when the function passed takes a second
// parameter: a Layout with query parameters cannot be used by another Load. The checker only says the
// types do not match.
func explainUse(msg string) string {
	if !strings.Contains(msg, "tifa.Use") || !layoutWithQueryRe.MatchString(msg) {
		return msg
	}
	return msg + ". A Layout with a second parameter, which holds query parameters, cannot be passed to tifa.Use: tifa.Use takes a Layout function of the form func(c *tifa.Ctx) (T, error). Take the data this Load needs from a Layout without query parameters, or read the parameters in this Load"
}

var importCycleRe = regexp.MustCompile(`^(could not import (\S+)) \(import cycle: \[([^\]]*)\]\)$`)

// explainCycle says what an import cycle error of the type checker means for an app. A package
// that imports itself is the usual mistake: the file already is in that package. Any other cycle
// is left as the type checker wrote it: how the app arranges its packages is its own choice.
func (a *analyzer) explainCycle(msg string) string {
	m := importCycleRe.FindStringSubmatch(msg)
	if m == nil {
		return msg
	}
	cycle := strings.Fields(m[3])
	if len(cycle) == 1 && cycle[0] == m[2] {
		return fmt.Sprintf("%s: this file is already in package %s, so it cannot import it. Drop the import and use the names of the package directly (import cycle: [%s])", m[1], m[2], m[3])
	}
	return msg
}

func (a *analyzer) relFile(name string) string {
	if rel, err := filepath.Rel(a.root, name); err == nil && filepath.IsAbs(name) && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return name
}

func (a *analyzer) errorAt(pos token.Pos, format string, args ...any) {
	p := a.fset.Position(pos)
	p.Filename = a.relFile(p.Filename)
	a.errs = append(a.errs, Error{Pos: p, Msg: fmt.Sprintf(format, args...)})
}

// errorInFile reports a violation that belongs to a whole file, at its line 1.
func (a *analyzer) errorInFile(id, file, format string, args ...any) {
	a.errs = append(a.errs, Error{Pos: token.Position{Filename: id + "/" + file, Line: 1}, Msg: fmt.Sprintf(format, args...)})
}

func (a *analyzer) sortedErrors() Errors {
	sort.SliceStable(a.errs, func(i, j int) bool {
		x, y := a.errs[i].Pos, a.errs[j].Pos
		if x.Filename != y.Filename {
			return x.Filename < y.Filename
		}
		if x.Line != y.Line {
			return x.Line < y.Line
		}
		if x.Column != y.Column {
			return x.Column < y.Column
		}
		return a.errs[i].Msg < a.errs[j].Msg
	})
	// A helper reached from several Loads is walked once for each of them.
	return slices.Compact(a.errs)
}

// route and layout hold what build needs beyond the manifest entries.
type route struct {
	Route
	file   string    // route.go, or page.tsx for a page without route.go, where violations of the whole route are reported
	segs   []segment // empty for the home page
	valid  bool      // the directory names a path, segs is set
	uses   []paramUse
	broken bool // the package does not type check, its Load and handlers are not known
}

type layout struct {
	Layout
	file  string // route.go when it declares the Layout function, else layout.tsx or layout_.tsx
	segs  []segment
	valid bool
	uses  []paramUse
}

// reservedQuery is the query parameter of the navigation data requests (see docs/build-and-deploy/build.md). The
// server takes it off the URL, so a Load could never read it.
const reservedQuery = "_tifa"

// paramUse is one c.Param or c.Query call with a constant name inside Load.
type paramUse struct {
	method string // "Param", "Query" or "QueryValues"
	name   string
	pos    token.Pos
}

func (a *analyzer) build() *Manifest {
	ids := make([]string, 0, len(a.dirs))
	for id := range a.dirs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var routes []*route
	var layouts []*layout
	for _, id := range ids {
		d := a.dirs[id]
		segs, valid := a.dirPath(d)
		a.checkPackageNames(d)
		pkg := a.routePackage(d)
		if l := a.layout(d, pkg); l != nil {
			l.segs, l.valid, l.Path = segs, valid, formatPath(segs)
			layouts = append(layouts, l)
		}
		if r := a.route(d, pkg); r != nil {
			r.segs, r.valid, r.Path = segs, valid, formatPath(segs)
			routes = append(routes, r)
		}
	}

	a.checkParamSiblings(ids)
	a.linkLayouts(routes, layouts)
	a.checkPaths(routes)
	a.checkLayoutURLs(routes, layouts)
	a.checkUses(layouts)
	a.checkBindTags()
	a.checkQueryTypes(routes, layouts)
	a.checkCacheIdentity(routes, layouts)

	i18n := a.i18n(routes, layouts)
	m := &Manifest{Module: a.module, Hooks: a.hooks(), Embedded: a.embed, Session: a.session(a.modulePackages), Settings: a.settings(), I18n: i18n, SiteURL: a.siteURL(), Content: a.content()}
	for _, l := range layouts {
		m.Layouts = append(m.Layouts, l.Layout)
	}
	for _, r := range routes {
		m.Routes = append(m.Routes, r.Route)
	}
	m.Warnings = a.bigSourceWarnings(m)
	return m
}

// firstFile is the first convention file of a directory, where violations of the directory as a whole are reported.
func (d *dirInfo) firstFile() string {
	switch {
	case d.routeGo:
		return "route.go"
	case d.pageTSX:
		return "page.tsx"
	case d.layoutTSX:
		return "layout.tsx"
	}
	return "layout_.tsx"
}

// dirPath reads the path of a directory and reports a name that is not a URL segment, or that
// the go command does not accept in an import path, such as a reserved Windows name or a
// Windows short name like x~1.
func (a *analyzer) dirPath(d *dirInfo) ([]segment, bool) {
	segs, err := dirSegments(d.id)
	if err == nil {
		// The directory may hold only TSX, so the module path is not always known. A fixed
		// prefix checks the part of the import path that comes from the directory.
		err = module.CheckImportPath("example.com/" + d.id)
	}
	if err != nil {
		a.errorInFile(d.id, d.firstFile(), "%v", err)
		return nil, false
	}
	return segs, true
}

// routeNames are the names that only route.go declares: the Layout of the directory, the Load of
// its page, the switches, and the method handlers.
var routeNames = append([]string{"Layout", "Load", "SSR", "Memo", "Cache"}, methods...)

// checkPackageNames reports Path, which the directory replaces, and each name of route.go that
// is declared in another file of the package. The files are read by name, so a Load in
// layout.go or a Cache in a helper file would otherwise be silently left out.
func (a *analyzer) checkPackageNames(d *dirInfo) {
	pkg := a.byDir[d.id]
	if pkg == nil {
		return
	}
	scope := pkg.Types.Scope()
	if obj := scope.Lookup("Path"); obj != nil {
		a.errorAt(obj.Pos(), "Path is not declared: the directory under app/ is the URL, write {name} as the directory name+ and {name...} as name++")
	}
	a.checkSwitchNames(scope)
	for _, name := range routeNames {
		obj := scope.Lookup(name)
		if obj == nil {
			continue
		}
		if _, isConst := obj.(*types.Const); name == "Cache" && !isConst {
			continue // a type or variable named Cache in another file is the domain code's own
		}
		if file := filepath.Base(a.fset.Position(obj.Pos()).Filename); file != "route.go" {
			a.errorAt(obj.Pos(), "%s is read from route.go only, move it there from %s", name, file)
		}
	}
}

// switchSpellings maps the lower case spelling of each switch to the advice for a constant that
// spells it so: the names of route.go are read as written, so a lower case one would be ignored.
var switchSpellings = map[string]string{
	"cache": "write Cache = true or Cache = 10 * time.Minute to let a shared cache keep the page, and Memo = false to turn the render memo off for it",
	"memo":  "write Memo = false to turn the render memo off for the page",
	"ssr":   "write SSR",
}

// checkSwitchNames reports the constants of route.go that look like a switch spelled wrong, since
// such a constant would otherwise be skipped without a word. A bool constant that spells a switch
// in the wrong case is reported with the advice for it. An exported constant of any type that is
// one or two letters off a switch, such as Cahce, is reported with the name it probably meant.
// A constant of another type spelled in lower case, such as const cache = "redis", and one in
// another file are the app's own and are left alone.
func (a *analyzer) checkSwitchNames(scope *types.Scope) {
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if !ok || filepath.Base(a.fset.Position(c.Pos()).Filename) != "route.go" {
			continue
		}
		if advice, ok := switchSpellings[name]; ok {
			if c.Val().Kind() == constant.Bool {
				a.errorAt(c.Pos(), "%s is not a switch: names are read as written and need a capital letter. %s", name, advice)
			}
			continue
		}
		if near := nearSwitch(c); near != "" {
			a.errorAt(c.Pos(), "%s is not a switch: did you mean %s?", name, near)
		}
	}
}

// switches are the constants of route.go that tifa reads, the ones a typo can be of.
var switches = []string{"SSR", "Cache", "Memo"}

// switchFits reports whether the constant c has a type the switch takes: a bool for SSR and Memo,
// a bool or a time.Duration for Cache. A constant of another type is the app's own, whatever its
// name.
func switchFits(sw string, c *types.Const) bool {
	switch sw {
	case "SSR", "Memo":
		return c.Val().Kind() == constant.Bool
	case "Cache":
		return c.Val().Kind() == constant.Bool || isNamed(c.Type(), "time", "Duration")
	}
	return false
}

// nearSwitch returns the switch that the constant c is a typo of, or "" when it is none. Only an
// exported name of at least three letters can be: tifa reads the names as written, and a short
// name such as ID or Max is the app's own. So is a constant whose type the switch does not take
// (const Demo = "demo"). It is one edit off a switch when the shorter of the two has up to four
// letters (Memoo for Memo, but not Mode), and up to two edits off with more (Cahce for Cache).
func nearSwitch(c *types.Const) string {
	name := c.Name()
	if len(name) < 3 || name[0] < 'A' || name[0] > 'Z' {
		return ""
	}
	best, bestDist := "", 0
	for _, s := range switches {
		limit := 2
		if min(len(name), len(s)) <= 4 {
			limit = 1
		}
		d := config.EditDistance(name, s)
		if d == 0 {
			return ""
		}
		if d <= limit && switchFits(s, c) && (best == "" || d < bestDist) {
			best, bestDist = s, d
		}
	}
	return best
}

// routePackage returns the package of the route.go in d, nil without route.go and when the
// package does not type check.
func (a *analyzer) routePackage(d *dirInfo) *packages.Package {
	if !d.routeGo || a.broken[d.id] {
		return nil
	}
	pkg := a.byDir[d.id]
	if pkg == nil {
		a.errorInFile(d.id, "route.go", "route.go is not part of a package the go command builds")
	}
	return pkg
}

// route reads the page or API route of d, nil when d has neither. A directory is a page when it
// has page.tsx, and an API route when route.go declares a method handler. route.go with only a
// Layout is neither. pkg is the package of route.go, nil when it has none or does not type check.
func (a *analyzer) route(d *dirInfo, pkg *packages.Package) *route {
	file := "route.go"
	if !d.routeGo {
		file = "page.tsx"
	}
	r := &route{Route: Route{ID: d.id + "/route", Dir: d.id, Page: d.pageTSX, SSR: true, Memo: true}, file: file}
	if !d.routeGo {
		if !d.pageTSX {
			return nil
		}
		return r
	}
	if a.broken[d.id] { // kept so that Layouts know a page is in scope
		if !d.pageTSX {
			return nil
		}
		r.broken = true
		return r
	}
	if pkg == nil {
		return nil
	}
	scope := pkg.Types.Scope()
	declared := slices.ContainsFunc(methods, func(m string) bool { return scope.Lookup(m) != nil })
	if load := scope.Lookup("Load"); load != nil && !d.pageTSX {
		a.errorAt(load.Pos(), "Load is not allowed without page.tsx: Load loads the data of a page, an API route answers GET with a GET function")
	}
	for _, name := range []string{"SSR", "Memo", "Cache"} {
		if obj := scope.Lookup(name); obj != nil && !d.pageTSX {
			a.errorAt(obj.Pos(), "%s is not allowed without page.tsx: it applies to the page of the folder only (a Layout has no switches of its own)", name)
		}
	}
	if !d.pageTSX && !declared {
		return nil
	}
	r.Package = pkg.PkgPath
	if d.pageTSX {
		r.Load, r.uses = a.readLoad(pkg, d.id, "Load")
	}
	for _, method := range methods {
		obj := scope.Lookup(method)
		if obj == nil {
			continue
		}
		if method == "GET" && d.pageTSX {
			a.errorAt(obj.Pos(), "GET is not allowed next to page.tsx: the framework renders GET for a page")
			continue
		}
		if h, ok := a.readHandler(pkg, obj, method); ok {
			r.Handlers = append(r.Handlers, h)
		}
	}
	r.SSR = a.boolConst(pkg, "SSR", true)
	r.Memo = a.memoConst(pkg)
	r.Cache, r.CacheDefault = a.cacheConst(pkg)
	return r
}

// layout reads the Layout of d, nil when d has none. A directory has a Layout when it has
// layout.tsx or when route.go declares Layout. pkg is the package of route.go, nil
// when it has none or does not type check.
func (a *analyzer) layout(d *dirInfo, pkg *packages.Package) *layout {
	l := &layout{Layout: Layout{ID: d.id + "/layout", Dir: d.id, TSX: d.layoutTSX || d.replaceTSX, Replace: d.replaces()}, file: "layout.tsx"}
	if d.replaceTSX && !d.layoutTSX {
		l.file = "layout_.tsx"
	}
	if pkg != nil {
		scope := pkg.Types.Scope()
		if scope.Lookup("Layout") != nil {
			l.file, l.Package = "route.go", pkg.PkgPath
			l.Load, l.uses = a.readLoad(pkg, d.id, "Layout")
			return l
		}
	}
	if !l.TSX {
		return nil
	}
	return l
}

// memoConst reads Memo, which can only be false: the render memo is on for every
// page, and the switch turns it off for one.
func (a *analyzer) memoConst(pkg *packages.Package) bool {
	obj := pkg.Types.Scope().Lookup("Memo")
	if obj == nil {
		return true
	}
	c, ok := obj.(*types.Const)
	if !ok || c.Val().Kind() != constant.Bool || constant.BoolVal(c.Val()) {
		a.errorAt(obj.Pos(), "Memo can only be false, which turns the render memo off for this page. It is on by default, so delete the line to keep it on")
		return true
	}
	return false
}

func (a *analyzer) boolConst(pkg *packages.Package, name string, def bool) bool {
	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return def
	}
	c, ok := obj.(*types.Const)
	if !ok || c.Val().Kind() != constant.Bool {
		a.errorAt(obj.Pos(), "%s must be a bool constant", name)
		return def
	}
	return constant.BoolVal(c.Val())
}

// checkFunc reports whether obj is a package-level function with exactly the given parameter
// and result types, and reports a violation otherwise.
func (a *analyzer) checkFunc(obj types.Object, params, results []string, want string) bool {
	fn, ok := obj.(*types.Func)
	if ok {
		sig := fn.Signature()
		ok = sig.TypeParams().Len() == 0 && !sig.Variadic() &&
			slices.Equal(tupleTypes(sig.Params()), params) && slices.Equal(tupleTypes(sig.Results()), results)
	}
	if !ok {
		a.errorAt(obj.Pos(), "%s must have the signature %s", obj.Name(), want)
	}
	return ok
}

func tupleTypes(t *types.Tuple) []string {
	var out []string
	for i := 0; i < t.Len(); i++ {
		out = append(out, typeString(t.At(i).Type()))
	}
	return out
}

// typeString prints t with full import paths, resolving aliases such as tifa.Ctx to what they name.
func typeString(t types.Type) string {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		return "*" + typeString(p.Elem())
	}
	if sig, ok := t.(*types.Signature); ok {
		out := "func(" + strings.Join(tupleTypes(sig.Params()), ", ") + ")"
		switch sig.Results().Len() {
		case 0:
		case 1:
			out += " " + typeString(sig.Results().At(0).Type())
		default:
			out += " (" + strings.Join(tupleTypes(sig.Results()), ", ") + ")"
		}
		return out
	}
	return types.TypeString(t, nil)
}

// readLoad reads the function that loads the data of a route (Load) or Layout (Layout): its
// result type, the fields of its query struct, the second parameter, and the constant parameter
// names passed to c.Param in its body, for checkPaths and linkLayouts to check against the path.
// owner is the directory of the route or Layout.
func (a *analyzer) readLoad(pkg *packages.Package, owner, name string) (*Load, []paramUse) {
	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return nil, nil
	}
	fn, ok := obj.(*types.Func)
	sig := (*types.Signature)(nil)
	if ok {
		sig = fn.Signature()
		params := sig.Params()
		ok = sig.TypeParams().Len() == 0 && !sig.Variadic() &&
			(params.Len() == 1 || params.Len() == 2) && typeString(params.At(0).Type()) == ctxType &&
			sig.Results().Len() == 2 && typeString(sig.Results().At(1).Type()) == "error"
	}
	if !ok {
		a.errorAt(obj.Pos(), "%s must have one of the signatures func(c *tifa.Ctx) (T, error) or func(c *tifa.Ctx, q Filter) (T, error), where Filter is a struct with a field for each query parameter the page reads", name)
		return nil, nil
	}
	result := sig.Results().At(0).Type()
	load := &Load{Type: typeString(result), Result: result}

	decl := a.funcDecl(pkg, fn)
	if sig.Params().Len() == 2 {
		a.readQuery(load, name, sig.Params().At(1).Type(), fieldTypePos(decl.Type.Params, 1))
	}
	problems, html := checkTSHTML(result)
	a.reportTS(fieldTypePos(decl.Type.Results, 0), name+" result", problems)
	load.HTML = html
	// The page leaves only data that is an object to the JSON text Go wrote (goJSON in
	// js/runtime/data.ts), so a marker in place of a bare value would never be used.
	// Pointers are written as what they point to, also those of a named pointer type.
	bare := types.Unalias(result)
	for p, ok := bare.Underlying().(*types.Pointer); ok; p, ok = bare.Underlying().(*types.Pointer) {
		bare = types.Unalias(p.Elem())
	}
	if isNamed(bare, tifaPath, "HTML") {
		a.errorAt(fieldTypePos(decl.Type.Results, 0), "%s result: a tifa.HTML on its own would come with the page twice, in its markup and in its data: put it in a struct field, and the data carries only a marker in its place", name)
	}

	return load, a.walkLoad(pkg, owner, name == "Layout", load, decl)
}

// queryAt is a field of the query struct of a Load, with where it is declared.
type queryAt struct {
	QueryField
	typ types.Type
	pos token.Pos
}

// readQuery reads the second parameter of a Load or Layout, the struct that the query string of
// the page is decoded into: the names of its fields are the query parameters of the page, and
// their types say how the values are read. what is "Load" or "Layout" and pos the place of the
// parameter type.
func (a *analyzer) readQuery(load *Load, what string, q types.Type, pos token.Pos) {
	st, isStruct := q.Underlying().(*types.Struct)
	if !isStruct || isNamed(q, multipartPath, "FileHeader") {
		a.errorAt(pos, "the second parameter of %s, %s, must be a struct, not a pointer or another type: it holds the query parameters, one field for each. For example type Filter struct { Page int `json:\"page\"` } and func %s(c *tifa.Ctx, q Filter) (T, error)", what, typeString(q), what)
		return
	}
	load.Input = typeString(q)
	label := what + " query"
	if !a.checkQueryStruct(label, q, st, nil, pos, "a "+what+" query comes from the query string") {
		return
	}
	// Validate belongs to the input of a method handler. A Load checks its values itself, where it can
	// answer in its own words.
	if fn, _, _ := types.LookupFieldOrMethod(q, true, nil, "Validate"); fn != nil {
		if _, isFunc := fn.(*types.Func); isFunc {
			a.errorAt(pos, "%s %s has a Validate method, which tifa runs only for the input of a method handler: check the values in %s and return tifa.Error{Status: 400, Message: \"page must be at least 1\"} or tifa.ErrNotFound for one that is not allowed", label, types.TypeString(q, shortQualifier), what)
			return
		}
	}
	for i := range st.NumFields() {
		f := st.Field(i)
		name := jsonName(st.Tag(i))
		if name == "-" || !f.Exported() {
			continue
		}
		if name == "" {
			name = f.Name()
		}
		field := QueryField{Name: name, Type: typeString(f.Type())}
		t := f.Type().Underlying()
		if s, ok := t.(*types.Slice); ok {
			field.List, t = true, s.Elem().Underlying()
		}
		if b := t.(*types.Basic); b.Kind() == types.Float32 {
			// The browser has no 32-bit float to write the shortest text of one, so it and Go could not read
			// a value alike.
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s.%s (%s) cannot hold a query value. A query value must be the same number in Go and in the browser, the browser has only 64-bit numbers, and a float32 rounds differently: ?price=1234567.89 is 1234567.9 in a float32, and the page would show 1234567.89. Declare it float64", label, rootLabel(q), f.Name(), types.TypeString(f.Type(), shortQualifier)), f))
			continue
		}
		field.Kind = queryKind(t.(*types.Basic)) // checkQueryStruct accepted the type
		load.Fields = append(load.Fields, field)
		load.Query = append(load.Query, name)
		a.queryAt[load] = append(a.queryAt[load], queryAt{QueryField: field, typ: f.Type(), pos: f.Pos()})
	}
	slices.SortFunc(load.Fields, func(x, y QueryField) int { return strings.Compare(x.Name, y.Name) })
	slices.Sort(load.Query)
	load.Query = slices.Compact(load.Query)
}

// queryKind names the kind of a basic type as the manifest says it, an alias such as byte or rune by
// the type it stands for.
func queryKind(b *types.Basic) string {
	switch b.Kind() {
	case types.Byte:
		return "uint8"
	case types.Rune:
		return "int32"
	}
	return b.Name()
}

// checkQueryStruct checks the struct the query string of a page is decoded into, the second
// parameter of a Load or Layout. See checkFlatInput for what it holds. files is nil: a page cannot
// hold a file.
// The name _tifa is the one the browser asks for the data of a navigation with, which the server
// removes before any code sees the request.
func (a *analyzer) checkQueryStruct(label string, in types.Type, st *types.Struct, files map[*types.Var]bool, pos token.Pos, why string) bool {
	ok := a.checkFlatInput(label, in, st, files, pos, why)
	named := map[string]*types.Var{}
	for i := range st.NumFields() {
		f := st.Field(i)
		if f.Embedded() || !f.Exported() || jsonName(st.Tag(i)) == "-" {
			continue
		}
		name := jsonName(st.Tag(i))
		if name == "" {
			name = f.Name()
		}
		if first := named[name]; first != nil {
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: the fields %s and %s are both the query parameter %q, and one value cannot fill two: give the parameter one field, or the other field its own json tag", label, first.Name(), f.Name(), name), f))
			ok = false
		} else {
			named[name] = f
		}
		if name == reservedQuery {
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: the query parameter %q is reserved for tifa: the browser asks for the data of a navigation with it, and the server removes it before any code sees the request. Rename the field, or its json tag", label, name), f))
			ok = false
		}
		if files[f] {
			continue
		}
		if files == nil && holdsFile(f.Type()) {
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s.%s is a file, which comes in a request body, and %s: take files in a POST, PUT or PATCH handler", label, rootLabel(in), f.Name(), why), f))
			ok = false
		}
	}
	return ok
}

func (a *analyzer) funcDecl(pkg *packages.Package, fn *types.Func) *ast.FuncDecl {
	for _, f := range pkg.Syntax {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && pkg.TypesInfo.Defs[fd.Name] == fn {
				return fd
			}
		}
	}
	panic("analyze: no declaration for " + fn.FullName()) // fn comes from this package's own scope
}

// ctxMethod returns "Param", "Query" or "QueryValues" when call invokes that method of tifa.Ctx, also through
// a struct that embeds the Ctx, and an empty string for any other call.
func ctxMethod(info *types.Info, call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if s := info.Selections[sel]; s == nil || s.Kind() != types.MethodVal {
		return ""
	}
	return paramMethod(info.Uses[sel.Sel])
}

// paramMethod returns "Param", "Query" or "QueryValues" when obj is that method of tifa.Ctx.
// QueryValues reads a query parameter like Query.
func paramMethod(obj types.Object) string {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Signature().Recv() == nil || fn.Name() != "Param" && fn.Name() != "Query" && fn.Name() != "QueryValues" {
		return ""
	}
	named, ok := types.Unalias(derefType(fn.Signature().Recv().Type())).(*types.Named)
	if ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == corePath && named.Obj().Name() == "Ctx" {
		return fn.Name()
	}
	return ""
}

// isUseFunc reports whether obj is tifa.Use.
func isUseFunc(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == tifaPath && fn.Name() == "Use"
}

// calleeIdent returns the identifier naming the function a call invokes: f in f(), x.f() and
// f[T](), nil for any other callee.
func calleeIdent(call *ast.CallExpr) *ast.Ident {
	fun := ast.Unparen(call.Fun)
	switch e := fun.(type) {
	case *ast.IndexExpr:
		fun = ast.Unparen(e.X)
	case *ast.IndexListExpr:
		fun = ast.Unparen(e.X)
	}
	switch e := fun.(type) {
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.Ident:
		return e
	}
	return nil
}

func derefType(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// isAncestor reports whether directory ID outer strictly contains directory ID inner.
func isAncestor(outer, inner string) bool { return strings.HasPrefix(inner, outer+"/") }

// covers reports whether a Layout in directory outer is over what is in directory inner: its own
// directory and every directory below it. It wraps them unless a layout_.tsx between drops it
// (see wraps).
func covers(outer, inner string) bool { return outer == inner || isAncestor(outer, inner) }

// dropper returns the Layout of the deepest layout_.tsx that covers dir and lies below the
// directory outer, which drops the Layout of outer for dir, nil when there is none. layouts is
// sorted by directory.
func dropper(layouts []*layout, outer, dir string) *layout {
	var found *layout
	for _, l := range layouts {
		if l.Replace && isAncestor(outer, l.Dir) && covers(l.Dir, dir) {
			found = l
		}
	}
	return found
}

// wraps reports whether the Layout l wraps what is in directory dir: it covers dir, and no
// layout_.tsx between drops it.
func wraps(layouts []*layout, l *layout, dir string) bool {
	return covers(l.Dir, dir) && dropper(layouts, l.Dir, dir) == nil
}

// linkLayouts fills Layout chains and scopes and checks that every path parameter a Layout
// function reads lies in the Layout's own path. A chain starts at the deepest layout_.tsx above
// a route, so the Layouts above it do not wrap the route.
func (a *analyzer) linkLayouts(routes []*route, layouts []*layout) {
	for _, l := range layouts {
		for _, p := range layouts { // sorted by directory, so the last match is the closest
			if !l.Replace && isAncestor(p.Dir, l.Dir) {
				l.Parent = p.ID
			}
		}
		pages := 0
		var dropped *layout // a layout_.tsx below l over a page that l would wrap without it
		for _, r := range routes {
			if !covers(l.Dir, r.Dir) {
				continue
			}
			if d := dropper(layouts, l.Dir, r.Dir); d != nil {
				if r.Page && dropped == nil {
					dropped = d
				}
				continue
			}
			l.Routes = append(l.Routes, r.ID)
			if r.Page {
				pages++
			}
		}
		if pages == 0 {
			if dropped != nil {
				a.errorInFile(l.Dir, l.file, "every page below this layout is below %s/layout_.tsx, which shows its pages without the layouts above it, so this layout wraps no page: add a page it wraps, or remove it", dropped.Dir)
			} else {
				a.errorInFile(l.Dir, l.file, "this layout has no pages (page.tsx) in its directory or below it")
			}
			continue
		}
		if !l.valid {
			continue
		}
		for _, u := range l.uses {
			if u.method == "Param" && !slices.ContainsFunc(l.segs, func(s segment) bool { return s.param == u.name }) {
				a.errorAt(u.pos, "c.Param(%q) reads a parameter that is not in %q, the path of the layout's directory %s", u.name, l.Path, l.Dir)
			}
		}
	}
	for _, r := range routes {
		for _, l := range layouts {
			if wraps(layouts, l, r.Dir) {
				r.Layouts = append(r.Layouts, l.ID)
			}
		}
	}
}

// checkParamSiblings reports a directory that holds more than one parameter directory (name+ or
// name++) with routes or Layouts in them, such as items/id+ next to items/key+: both would read
// the same URL segment. ids lists the directories with convention files, sorted. Each extra
// parameter directory is reported once, at the first convention file in or below it. A
// directory whose path is not valid is left to dirPath.
func (a *analyzer) checkParamSiblings(ids []string) {
	first := map[string]string{} // parent directory to its first parameter directory
	reported := map[string]bool{}
	for _, id := range ids {
		segs, err := dirSegments(id)
		if err != nil {
			continue
		}
		parts := strings.Split(id, "/") // "app" and one part per segment
		for i, seg := range segs {
			if seg.param == "" {
				continue
			}
			parent, dir := strings.Join(parts[:i+1], "/"), strings.Join(parts[:i+2], "/")
			other, ok := first[parent]
			if !ok || other == dir {
				first[parent] = dir
				continue
			}
			if !reported[dir] {
				reported[dir] = true
				a.errorInFile(id, a.dirs[id].firstFile(), "%s and %s are both parameter directories in %s: a directory holds at most one, since both would read the same URL segment", other, dir, parent)
			}
		}
	}
}

// checkPaths reports c.Param names that are not in the route's own path, and every pair of
// paths that ServeMux would refuse to register together: a conflict when they share some URLs
// and neither is more specific. Two paths that match the same URLs differ only in parameter
// names, so they go through two parameter directories in one directory, which
// checkParamSiblings reports. Each pair is reported once, at the later route.
func (a *analyzer) checkPaths(routes []*route) {
	var checked []*route
	for _, r := range routes {
		if !r.valid {
			continue
		}
		for _, u := range r.uses {
			if u.method == "Param" && !slices.ContainsFunc(r.segs, func(s segment) bool { return s.param == u.name }) {
				a.errorAt(u.pos, "c.Param(%q) reads a parameter that is not in %q, the path of %s", u.name, r.Path, r.Dir)
			}
		}
		for _, other := range checked {
			if comparePaths(muxSegments(other.segs), muxSegments(r.segs)) == overlaps {
				a.errorInFile(r.Dir, r.file, "the path %q conflicts with %q of %s: both match some URLs and neither is more specific", r.Path, other.Path, other.Dir)
			}
		}
		checked = append(checked, r)
	}
	// The server also registers the prefix of a route for the rest of the path, /files for
	// /files/{path...}, unless a route has that path.
	for _, r := range checked {
		prefix, ok := restPrefix(r.segs)
		if !ok || slices.ContainsFunc(checked, func(o *route) bool { return o.Path == formatPath(prefix) }) {
			continue
		}
		for _, other := range checked {
			if other != r && comparePaths(muxSegments(prefix), muxSegments(other.segs)) == overlaps {
				a.errorInFile(r.Dir, r.file, "the path %q also answers %q, which conflicts with %q of %s: both match some URLs and neither is more specific", r.Path, formatPath(prefix), other.Path, other.Dir)
			}
		}
	}
}

// checkLayoutURLs reports a page that matches the URL of a Layout with no page there. The
// browser router matches a Layout with a path at its own URL and shows the root's NotFound
// there, so a page that Go would serve at that URL never shows in the browser. A page in the
// Layout's directory, or for the rest of the path right below it, owns the URL instead.
func (a *analyzer) checkLayoutURLs(routes []*route, layouts []*layout) {
	for _, l := range layouts {
		if !l.valid || len(l.segs) == 0 {
			continue
		}
		owned := slices.ContainsFunc(routes, func(r *route) bool {
			prefix, ok := restPrefix(r.segs)
			return r.Page && (r.Dir == l.Dir || ok && path.Dir(r.Dir) == l.Dir && len(prefix) == len(l.segs))
		})
		if owned {
			continue
		}
		for _, r := range routes {
			if !r.Page || !r.valid {
				continue
			}
			paths := [][]segment{r.segs}
			if prefix, ok := restPrefix(r.segs); ok {
				paths = append(paths, prefix)
			}
			if slices.ContainsFunc(paths, func(segs []segment) bool {
				rel := comparePaths(muxSegments(l.segs), muxSegments(segs))
				return rel == equivalent || rel == moreSpecific || rel == overlaps
			}) {
				a.errorInFile(l.Dir, l.file, "the URL %q of the layout of %s has no page, but the page %s matches it: the browser would show the 404 page there while the server renders %s, so add a page.tsx to %s or rename one of the directories", l.Path, l.Dir, r.Dir, r.Dir, l.Dir)
			}
		}
	}
}

// restPrefix returns the segments before the rest of the path {name...} that ends segs, which
// the route also answers with an empty rest. A Path of only the rest has no such prefix.
func restPrefix(segs []segment) ([]segment, bool) {
	if len(segs) < 2 || !segs[len(segs)-1].wildcard {
		return nil, false
	}
	return segs[:len(segs)-1], true
}

// checkUses finds every call of tifa.Use in the loaded packages. A function literal or a method
// value as its argument is refused, because the runtime tells Loads apart by code address and
// closures of one literal or method values of one method would share results. A package-level
// function must be the Layout function of a Layout that wraps the calling package: in its directory
// or above it. Any other argument, such as a variable, is left alone: the build cannot see what it
// holds.
func (a *analyzer) checkUses(layouts []*layout) {
	layoutByPkg := map[string]*layout{}
	for _, l := range layouts {
		if l.Package != "" {
			layoutByPkg[l.Package] = l
		}
	}
	for _, pkg := range a.pkgs {
		caller := a.id(filepath.Dir(pkg.GoFiles[0]))
		for _, f := range pkg.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && isTifaUse(pkg.TypesInfo, call) {
					a.checkUse(pkg.TypesInfo, call, caller, layouts, layoutByPkg)
				}
				return true
			})
		}
	}
	// A helper outside the Load's package may call tifa.Use on its behalf. The Layout must then
	// be an ancestor of the route or Layout whose Load reaches the helper.
	for _, u := range a.helperUses {
		a.checkUse(u.info, u.call, u.caller, layouts, layoutByPkg)
	}
}

func (a *analyzer) checkUse(info *types.Info, call *ast.CallExpr, caller string, layouts []*layout, layoutByPkg map[string]*layout) {
	if len(call.Args) != 2 {
		return
	}
	arg := ast.Unparen(call.Args[1])
	text := types.ExprString(arg)
	const need = "the function passed to tifa.Use must be a package-level function"
	switch e := arg.(type) {
	case *ast.FuncLit:
		a.errorAt(arg.Pos(), "%s, got a closure: tifa.Use tells Loads apart by code address, so closures of one literal would share results", need)
		return
	case *ast.SelectorExpr:
		if info.Selections[e] != nil {
			a.errorAt(arg.Pos(), "%s, got the method value %s: method values of one method share a code address and would share results", need, text)
			return
		}
	}
	fn := packageFunc(info, arg)
	if fn == nil {
		return // a variable or the result of a call: the build cannot see which function it holds
	}
	l := layoutByPkg[fn.Pkg().Path()]
	if l == nil || fn.Name() != "Layout" {
		a.errorAt(arg.Pos(), "tifa.Use(c, %s): %s is not the Layout function of a layout", text, text)
		return
	}
	if !covers(l.Dir, caller) {
		a.errorAt(arg.Pos(), "tifa.Use(c, %s): the layout of %s does not wrap %s", text, l.Dir, caller)
		return
	}
	if d := dropper(layouts, l.Dir, caller); d != nil {
		a.errorAt(arg.Pos(), "tifa.Use(c, %s): %s/layout_.tsx drops the layout of %s, so its Layout does not run for %s: call the code %s calls yourself, or rename layout_.tsx to layout.tsx to keep the layouts above", text, d.Dir, l.Dir, caller, text)
	}
}

// packageFunc returns the package-level function that expr names, nil for anything else.
func packageFunc(info *types.Info, expr ast.Expr) *types.Func {
	var fn *types.Func
	switch e := ast.Unparen(expr).(type) {
	case *ast.SelectorExpr:
		if info.Selections[e] != nil { // a method value
			return nil
		}
		fn, _ = info.Uses[e.Sel].(*types.Func)
	case *ast.Ident:
		fn, _ = info.Uses[e].(*types.Func)
	}
	if fn == nil || fn.Signature().Recv() != nil || fn.Pkg() == nil || fn.Parent() != fn.Pkg().Scope() {
		return nil
	}
	return fn
}

func isTifaUse(info *types.Info, call *ast.CallExpr) bool {
	id := calleeIdent(call)
	return id != nil && isUseFunc(info.Uses[id])
}

// hooks lists the hooks that the root package defines and checks their signatures.
func (a *analyzer) hooks() []string {
	if a.rootPk == nil { // no hooks.go, or the root package does not type check
		return nil
	}
	var found []string
	for _, h := range hooks {
		obj := a.rootPk.Types.Scope().Lookup(h.name)
		if obj != nil && a.checkFunc(obj, h.params, h.results, h.signature) {
			found = append(found, h.name)
		}
	}
	return found
}

// cacheConst reads Cache, which lets a shared cache in front of the program keep the page. It is true,
// which leaves the time to the setting cache.s_maxage (the second result), false for none, or
// a time.Duration constant of at least a second, such as 10 * time.Minute (the first result). Both are
// zero/false when the page does not set it.
func (a *analyzer) cacheConst(pkg *packages.Package) (time.Duration, bool) {
	obj := pkg.Types.Scope().Lookup("Cache")
	if obj == nil {
		return 0, false
	}
	c, isConst := obj.(*types.Const)
	if isConst && c.Val().Kind() == constant.Bool {
		return 0, constant.BoolVal(c.Val())
	}
	named, isNamed := types.Unalias(obj.Type()).(*types.Named)
	isDuration := isNamed && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "time" && named.Obj().Name() == "Duration"
	if !isConst && !isDuration {
		return 0, false // a type or a function named Cache is the domain code's own
	}
	if !isConst || !isDuration {
		a.errorAt(obj.Pos(), "Cache must be true, false or a time.Duration constant, such as const Cache = 10 * time.Minute")
		return 0, false
	}
	ns, exact := constant.Int64Val(c.Val())
	if !exact || time.Duration(ns) < time.Second {
		a.errorAt(obj.Pos(), "Cache must be at least one second, such as const Cache = 10 * time.Minute")
		return 0, false
	}
	return time.Duration(ns), false
}

// checkCacheIdentity reports the reads of cookies, headers and the session in code that runs for
// pages with Cache alone, the functions it calls included: the Load of such a page, and the Layout
// function of a Layout whose pages all have Cache. The request of such a page is anonymous, so the
// read is always empty there, which is a mistake in the code and not a visitor without a cookie. A
// read that a page without Cache reaches too, such as one in a Layout shared with it or in a helper
// its Load calls, gives that page what it asks for and the page with Cache the anonymous answer,
// which is right for a page that is the same for everyone, so it is left alone.
func (a *analyzer) checkCacheIdentity(routes []*route, layouts []*layout) {
	pages := map[string]*route{}
	for _, r := range routes {
		if r.Page {
			pages[r.ID] = r
		}
	}
	// why says, for each Load, whether it runs for pages with Cache alone and which.
	why := map[*Load]string{}
	for _, r := range routes {
		if r.Page && r.Load != nil && r.Shared() {
			why[r.Load] = r.Dir + " has Cache"
		}
	}
	for _, l := range layouts {
		if l.Load == nil {
			continue
		}
		shared := 0
		for _, id := range l.Routes {
			if p := pages[id]; p != nil && !p.Shared() {
				shared = -1
				break
			} else if p != nil {
				shared++
			}
		}
		if shared > 0 {
			why[l.Load] = "every page below " + l.Dir + " has Cache"
		}
	}
	type read struct {
		identityRead
		why   string
		mixed bool // a Load that does not run for pages with Cache alone reaches it too
	}
	var reads []*read
	byPos := map[token.Pos]*read{}
	for load, list := range a.identity {
		for _, r := range list {
			x := byPos[r.pos]
			if x == nil {
				x = &read{identityRead: r}
				byPos[r.pos] = x
				reads = append(reads, x)
			}
			if w, ok := why[load]; !ok {
				x.mixed = true
			} else if x.why == "" || w < x.why {
				x.why = w // the same text whatever the order of the map
			}
		}
	}
	for _, r := range reads {
		if r.mixed {
			continue
		}
		what := r.what + " is always empty here"
		if r.what == "c.Header" {
			what = "c.Header sees only Host, Accept-Encoding, If-None-Match, If-Modified-Since, Cache-Control, Pragma, Content-Length, Connection, Origin and Sec-Fetch-* here, every other header is always empty"
		}
		a.errorAt(r.pos, "%s: %s, so its requests are anonymous and the answer is kept for everyone. Remove Cache, or move what depends on the visitor into the browser, where the page can read it after it loads", what, r.why)
	}
}

// checkQueryTypes requires a query parameter that a Layout and a page below it, or two Layouts of one
// chain, both declare to have the same type. The browser reads the query string once for the whole
// chain, and a Layout hands its values on to the routes below it, so one name has one meaning.
func (a *analyzer) checkQueryTypes(routes []*route, layouts []*layout) {
	byID := map[string]*layout{}
	for _, l := range layouts {
		byID[l.ID] = l
	}
	type pair struct{ outer, inner token.Pos }
	reported := map[pair]bool{}
	for _, r := range routes {
		if r.broken {
			continue
		}
		var chain []*Load
		for _, id := range r.Layouts {
			if l := byID[id]; l != nil && l.Load != nil {
				chain = append(chain, l.Load)
			}
		}
		if r.Page && r.Load != nil {
			chain = append(chain, r.Load)
		}
		for i, inner := range chain {
			for _, outer := range chain[:i] {
				for _, in := range a.queryAt[inner] {
					for _, out := range a.queryAt[outer] {
						if in.Name != out.Name || types.Identical(in.typ, out.typ) || reported[pair{out.pos, in.pos}] {
							continue
						}
						reported[pair{out.pos, in.pos}] = true
						at := a.fset.Position(out.pos)
						a.errorAt(in.pos, "the query parameter %q has the type %s here and %s at %s:%d: a Layout and the pages below it read one query string, so a name they share must have one type. Change one of the fields to the type of the other", in.Name, types.TypeString(in.typ, shortQualifier), types.TypeString(out.typ, shortQualifier), a.relFile(at.Filename), at.Line)
					}
				}
			}
		}
	}
}

// checkBindTags reports a form tag in a struct passed to c.Bind or c.BindQuery, as it does for the
// input of a typed handler: those read a field by its json tag only. The argument is checked by its
// static type, so a value behind an interface or built by reflection is not seen.
func (a *analyzer) checkBindTags() {
	for _, pkg := range a.pkgs {
		for _, f := range pkg.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Bind" && sel.Sel.Name != "BindQuery") {
					return true
				}
				fn, ok := pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
				if !ok || fn.Signature().Recv() == nil {
					return true
				}
				if named, ok := types.Unalias(derefType(fn.Signature().Recv().Type())).(*types.Named); !ok || !isCtxNamed(named) {
					return true
				}
				t := pkg.TypesInfo.TypeOf(call.Args[0])
				if t == nil {
					return true
				}
				root := types.ExprString(call.Args[0])
				root = strings.TrimPrefix(root, "&")
				walkFields(t, root, map[types.Type]bool{}, func(path string, field *types.Var, tag string) {
					if _, has := reflect.StructTag(tag).Lookup("form"); has {
						a.errorAt(call.Pos(), "%s", a.withField(fmt.Sprintf("c.%s: %s has a form tag: c.%s names a field by its json tag only, as a typed handler input does, so remove the form tag", sel.Sel.Name, path, sel.Sel.Name), field))
					}
				})
				return true
			})
		}
	}
}
