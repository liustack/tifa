package analyze

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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
	{"OnStart", nil, []string{"error"}, "func() error"},
	{"OnShutdown", []string{"context.Context"}, []string{"error"}, "func(ctx context.Context) error"},
	{"OnRequest", []string{ctxType}, []string{"error"}, "func(c *tifa.Ctx) error"},
	{"OnError", []string{ctxType, "error"}, []string{corePath + ".Error"}, "func(c *tifa.Ctx, err error) tifa.Error"},
}

// Analyze loads the application whose module root is dir and returns its route manifest.
// Convention violations come back as Errors, holding every violation found, sorted by position.
// Packages that do not type check, anywhere in the import graph, are reported with their errors.
// The app packages among them and those importing them, directly or through packages outside
// app/, are skipped. The conventions of all other packages are still checked.
// Other errors (no app directory, go list failing) are returned as they are.
func Analyze(dir string) (*Manifest, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	a := &analyzer{root: root, dirs: map[string]*dirInfo{}, used: usedLoads{}}
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

type analyzer struct {
	root   string
	module string
	fset   *token.FileSet
	dirs   map[string]*dirInfo // by ID
	pkgs   []*packages.Package // app packages and the root package
	byDir  map[string]*packages.Package
	rootPk *packages.Package // nil without hooks.go
	broken map[string]bool   // directory IDs of packages skipped because they or their imports do not type check
	// appPkgs holds the import paths of pkgs, modulePkgs those of every package in the module.
	appPkgs, modulePkgs map[string]bool
	funcs               map[string]funcSource // functions of module packages that type check, by full name
	checked             map[string]bool       // followed functions whose own body has been checked
	carrying            map[*types.Named]bool // carriesCtx results
	helperUses          []useCall
	errs                Errors
	used                usedLoads
}

// dirInfo records the convention files found in one directory under app/.
type dirInfo struct {
	id                                     string
	routeGo, routeTSX, layoutGo, layoutTSX bool
}

// conventionFiles are the files that make a directory under app/ a route or a Layout.
var conventionFiles = []string{"route.go", "route.tsx", "layout.go", "layout.tsx"}

// scan walks app/ and records which convention files exist. The go command skips directories
// whose name starts with "." or "_" and directories named testdata, so a convention file below
// one of them is reported instead of silently left out.
func (a *analyzer) scan() error {
	appDir := filepath.Join(a.root, "app")
	if info, err := os.Stat(appDir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s has no app directory", a.root)
	}
	skipped := "" // the outermost skipped directory being walked, by ID
	return filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			id := a.id(path)
			if skipped != "" && !isAncestor(skipped, id) {
				skipped = ""
			}
			if skipped == "" && path != appDir && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
				skipped = id
			}
			return nil
		}
		if !slices.Contains(conventionFiles, name) {
			return nil
		}
		id := a.id(filepath.Dir(path))
		if skipped != "" {
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
		case "route.tsx":
			info.routeTSX = true
		case "layout.go":
			info.layoutGo = true
		case "layout.tsx":
			info.layoutTSX = true
		}
		return nil
	})
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
	hasHooks := false
	if _, err := os.Stat(filepath.Join(a.root, "hooks.go")); err == nil {
		hasHooks = true
	}
	a.fset = token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
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
	// The application is the packages under app/ and, with hooks.go, the root package. Other
	// packages of the module matter only through what the application imports.
	var roots []*packages.Package
	a.modulePkgs = map[string]bool{}
	for _, p := range pkgs {
		if len(p.GoFiles) == 0 {
			continue
		}
		if p.Module != nil && a.module == "" {
			a.module = p.Module.Path
		}
		a.modulePkgs[p.PkgPath] = true
		id := a.id(filepath.Dir(p.GoFiles[0]))
		if id == "app" || strings.HasPrefix(id, "app/") || id == "." && hasHooks {
			roots = append(roots, p)
		}
	}
	a.byDir = map[string]*packages.Package{}
	a.broken = map[string]bool{}
	a.appPkgs = map[string]bool{}
	a.funcs = map[string]funcSource{}
	a.checked = map[string]bool{}
	a.carrying = map[*types.Named]bool{}
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
		if id == "." {
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
	file   string    // route.go, or route.tsx for a page without route.go, where violations of the whole route are reported
	segs   []segment // empty for the home page
	valid  bool      // the directory names a path, segs is set
	uses   []paramUse
	broken bool // the package does not type check, its Load and handlers are not known
}

type layout struct {
	Layout
	file  string // layout.go or layout.tsx
	segs  []segment
	valid bool
	uses  []paramUse
}

// usedLoads maps a Load to the import paths of the packages whose Layout function it passes to tifa.Use.
type usedLoads map[*Load][]string

// paramUse is one c.Param or c.Query call with a constant name inside Load.
type paramUse struct {
	method string // "Param" or "Query"
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
		if d.layoutGo || d.layoutTSX {
			if l := a.layout(d); l != nil {
				l.segs, l.valid, l.Path = segs, valid, formatPath(segs)
				layouts = append(layouts, l)
			}
		}
		if d.routeGo || d.routeTSX {
			if r := a.route(d); r != nil {
				r.segs, r.valid, r.Path = segs, valid, formatPath(segs)
				routes = append(routes, r)
			}
		}
	}

	a.linkLayouts(routes, layouts)
	a.checkPaths(routes)
	a.checkUses(layouts)
	a.checkDirectCalls()
	a.propagateQuery(routes, layouts)

	m := &Manifest{Module: a.module, Hooks: a.hooks()}
	for _, l := range layouts {
		m.Layouts = append(m.Layouts, l.Layout)
	}
	for _, r := range routes {
		m.Routes = append(m.Routes, r.Route)
	}
	return m
}

// firstFile is the first convention file of a directory, where violations of the directory as a whole are reported.
func (d *dirInfo) firstFile() string {
	switch {
	case d.routeGo:
		return "route.go"
	case d.layoutGo:
		return "layout.go"
	case d.routeTSX:
		return "route.tsx"
	}
	return "layout.tsx"
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

// routeNames are the names that only route.go declares. route.go and layout.go of one
// directory share a package, so a Layout has names of its own: Layout and LayoutSSR.
var routeNames = append([]string{"Load", "SSR", "Cache"}, methods...)

// checkPackageNames reports Path, which the directory replaces, and the names of route.go in a
// directory that has no route, so that a Layout written the old way, with Load and SSR, is not
// silently left without data.
func (a *analyzer) checkPackageNames(d *dirInfo) {
	pkg := a.byDir[d.id]
	if pkg == nil {
		return
	}
	scope := pkg.Types.Scope()
	if obj := scope.Lookup("Path"); obj != nil {
		a.errorAt(obj.Pos(), "Path is not declared: the directory under app/ is the URL, write {name} as the directory name+ and {name...} as name++")
	}
	if d.routeGo || d.routeTSX {
		return
	}
	for _, name := range routeNames {
		obj := scope.Lookup(name)
		switch {
		case obj == nil:
		case name == "Load":
			a.errorAt(obj.Pos(), "Load loads the data of a page in route.go: a Layout loads its data with func Layout(c *tifa.Ctx) (T, error)")
		case name == "SSR":
			a.errorAt(obj.Pos(), "SSR switches server rendering of a page in route.go: a Layout declares LayoutSSR")
		default:
			a.errorAt(obj.Pos(), "a layout does not declare %s, it belongs in route.go", name)
		}
	}
}

func (a *analyzer) route(d *dirInfo) *route {
	file := "route.go"
	if !d.routeGo {
		file = "route.tsx"
	}
	r := &route{Route: Route{ID: d.id + "/route", Dir: d.id, Page: d.routeTSX, SSR: true}, file: file}
	if !d.routeGo {
		return r
	}
	if a.broken[d.id] { // kept so that Layouts know a page is in scope
		r.broken = true
		return r
	}
	pkg := a.byDir[d.id]
	if pkg == nil {
		a.errorInFile(d.id, "route.go", "route.go is not part of a package the go command builds")
		return nil
	}
	r.Package = pkg.PkgPath
	scope := pkg.Types.Scope()
	r.Load, r.uses = a.readLoad(pkg, d.id, "Load")
	if r.Load != nil && !d.routeTSX {
		a.errorAt(scope.Lookup("Load").Pos(), "Load is not allowed without route.tsx: Load loads the data of a page, an API route answers GET with a GET function")
		r.Load, r.uses = nil, nil
	}
	for _, method := range methods {
		obj := scope.Lookup(method)
		if obj == nil {
			continue
		}
		if !a.checkFunc(obj, []string{ctxType}, []string{"error"}, "func(c *tifa.Ctx) error") {
			continue
		}
		if method == "GET" && d.routeTSX {
			a.errorAt(obj.Pos(), "GET is not allowed next to route.tsx: the framework renders GET for a page")
			continue
		}
		r.Methods = append(r.Methods, method)
	}
	r.SSR = a.boolConst(pkg, "SSR", true)
	r.Cache = a.boolConst(pkg, "Cache", false)
	return r
}

func (a *analyzer) layout(d *dirInfo) *layout {
	file := "layout.go"
	if !d.layoutGo {
		file = "layout.tsx"
	}
	l := &layout{Layout: Layout{ID: d.id + "/layout", Dir: d.id, TSX: d.layoutTSX, SSR: true}, file: file}
	if !d.layoutGo || a.broken[d.id] {
		return l
	}
	pkg := a.byDir[d.id]
	if pkg == nil {
		a.errorInFile(d.id, "layout.go", "layout.go is not part of a package the go command builds")
		return nil
	}
	l.Package = pkg.PkgPath
	l.Load, l.uses = a.readLoad(pkg, d.id, "Layout")
	l.SSR = a.boolConst(pkg, "LayoutSSR", true)
	return l
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
	return types.TypeString(t, nil)
}

// readLoad reads the function that loads the data of a route (Load) or Layout (Layout): its
// result type and the constant parameter names passed to c.Param and c.Query in its body.
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
		ok = sig.TypeParams().Len() == 0 && !sig.Variadic() &&
			slices.Equal(tupleTypes(sig.Params()), []string{ctxType}) &&
			sig.Results().Len() == 2 && typeString(sig.Results().At(1).Type()) == "error"
	}
	if !ok {
		a.errorAt(obj.Pos(), "%s must have the signature func(c *tifa.Ctx) (T, error)", name)
		return nil, nil
	}
	result := sig.Results().At(0).Type()
	load := &Load{Type: typeString(result), Result: result}

	decl := a.funcDecl(pkg, fn)
	resultPos := decl.Type.Results.List[0].Type.Pos()
	for _, p := range checkTS(result) {
		msg := fmt.Sprintf("%s result: %s: %s", name, p.path, p.msg)
		if p.field != nil && p.field.Pos().IsValid() {
			fp := a.fset.Position(p.field.Pos())
			msg += fmt.Sprintf(" (field declared at %s:%d)", a.relFile(fp.Filename), fp.Line)
		}
		a.errorAt(resultPos, "%s", msg)
	}

	uses := a.walkLoad(pkg, owner, load, decl, sig)
	for _, u := range uses {
		if u.method == "Param" {
			load.Params = append(load.Params, u.name)
		} else {
			load.Query = append(load.Query, u.name)
		}
	}
	slices.Sort(load.Params)
	load.Params = slices.Compact(load.Params)
	slices.Sort(load.Query)
	load.Query = slices.Compact(load.Query)
	return load, uses
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

// ctxMethod returns "Param" or "Query" when call invokes that method of tifa.Ctx, also through
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

// paramMethod returns "Param" or "Query" when obj is that method of tifa.Ctx.
func paramMethod(obj types.Object) string {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Signature().Recv() == nil || fn.Name() != "Param" && fn.Name() != "Query" {
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

// checkDirectCalls requires c.Param, c.Query and tifa.Use to be called directly, in every loaded
// package. The analyzer reads parameter names and Use arguments at the call site, so a method
// value (f := c.Query), a method expression ((*tifa.Ctx).Query(c, "x")) or tifa.Use stored in a
// variable or passed as an argument would hide them.
func (a *analyzer) checkDirectCalls() {
	for _, pkg := range a.pkgs {
		for _, f := range pkg.Syntax {
			a.checkDirectCallsIn(pkg.TypesInfo, f)
		}
	}
}

// checkDirectCallsIn applies checkDirectCalls to the syntax under root.
func (a *analyzer) checkDirectCallsIn(info *types.Info, root ast.Node) {
	called := map[*ast.Ident]bool{}
	selectors := map[*ast.Ident]*ast.SelectorExpr{}
	var idents []*ast.Ident
	ast.Inspect(root, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.CallExpr:
			if id := calleeIdent(e); id != nil {
				called[id] = true
			}
		case *ast.SelectorExpr:
			selectors[e.Sel] = e
		case *ast.Ident:
			idents = append(idents, e)
		}
		return true
	})
	for _, id := range idents {
		obj := info.Uses[id]
		method := paramMethod(obj)
		var name string
		switch {
		case method != "":
			name = "c." + method
			if sel := selectors[id]; sel != nil && info.Selections[sel] != nil && info.Selections[sel].Kind() == types.MethodExpr {
				a.errorAt(sel.Pos(), "%s is a method expression: call c.%s(...) directly, the analyzer reads parameter names at the call", types.ExprString(sel), method)
				continue
			}
		case isUseFunc(obj):
			name = "tifa.Use"
		default:
			continue
		}
		if !called[id] {
			pos := id.Pos()
			if sel := selectors[id]; sel != nil {
				pos = sel.Pos()
			}
			a.errorAt(pos, "%s must be called directly, not stored or passed as a value, so the analyzer can read its arguments", name)
		}
	}
}

func derefType(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// isAncestor reports whether directory ID outer strictly contains directory ID inner.
func isAncestor(outer, inner string) bool { return strings.HasPrefix(inner, outer+"/") }

// covers reports whether a Layout in directory outer wraps what is in directory inner: its own
// directory and every directory below it.
func covers(outer, inner string) bool { return outer == inner || isAncestor(outer, inner) }

// linkLayouts fills Layout chains and scopes and checks that every path parameter a Layout
// function reads lies in the Layout's own path.
func (a *analyzer) linkLayouts(routes []*route, layouts []*layout) {
	for _, l := range layouts {
		for _, p := range layouts { // sorted by directory, so the last match is the closest
			if isAncestor(p.Dir, l.Dir) {
				l.Parent = p.ID
			}
		}
		pages := 0
		for _, r := range routes {
			if covers(l.Dir, r.Dir) {
				l.Routes = append(l.Routes, r.ID)
				if r.Page {
					pages++
				}
			}
		}
		if pages == 0 {
			a.errorInFile(l.Dir, l.file, "this layout has no pages (route.tsx) in its directory or below it")
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
			if covers(l.Dir, r.Dir) {
				r.Layouts = append(r.Layouts, l.ID)
				if !l.SSR {
					r.SSR = false
				}
			}
		}
	}
}

// checkPaths reports c.Param names that are not in the route's own path, and every pair of
// paths that ServeMux would refuse to register together: a duplicate when they match the same
// URLs, such as the sibling directories id+ and key+, and a conflict when they share some URLs
// and neither is more specific. Each pair is reported once, at the later route.
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
			switch comparePaths(muxSegments(other.segs), muxSegments(r.segs)) {
			case equivalent:
				a.errorInFile(r.Dir, r.file, "the path %q duplicates %q of %s: both match the same URLs", r.Path, other.Path, other.Dir)
			case overlaps:
				a.errorInFile(r.Dir, r.file, "the path %q conflicts with %q of %s: both match some URLs and neither is more specific", r.Path, other.Path, other.Dir)
			}
		}
		checked = append(checked, r)
	}
}

// checkUses finds every call of tifa.Use in the loaded packages. Its function argument must be
// a package-level function, because the runtime tells Loads apart by code address and closures
// of one literal or method values of one method would share results. It must also be the Layout
// function of a Layout that wraps the calling package: in its directory or above it.
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
					a.checkUse(pkg.TypesInfo, call, caller, layoutByPkg)
				}
				return true
			})
		}
	}
	// A helper outside the Load's package may call tifa.Use on its behalf. The Layout must then
	// be an ancestor of the route or Layout whose Load reaches the helper.
	for _, u := range a.helperUses {
		a.checkUse(u.info, u.call, u.caller, layoutByPkg)
	}
}

func (a *analyzer) checkUse(info *types.Info, call *ast.CallExpr, caller string, layoutByPkg map[string]*layout) {
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
		a.errorAt(arg.Pos(), "%s, got %s", need, text)
		return
	}
	l := layoutByPkg[fn.Pkg().Path()]
	if l == nil || fn.Name() != "Layout" {
		a.errorAt(arg.Pos(), "tifa.Use(c, %s): %s is not the Layout function of a layout", text, text)
		return
	}
	if !covers(l.Dir, caller) {
		a.errorAt(arg.Pos(), "tifa.Use(c, %s): the layout of %s does not wrap %s", text, l.Dir, caller)
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

// propagateQuery fills Uses and AllQuery of every Load. tifa.Use only reaches ancestor Layouts,
// so the graph has no cycles once checkUses passes. The visiting set still guards against them
// when checkUses has reported a violation.
func (a *analyzer) propagateQuery(routes []*route, layouts []*layout) {
	byPkg := map[string]*layout{}
	for _, l := range layouts {
		if l.Package != "" && l.Load != nil {
			byPkg[l.Package] = l
		}
	}
	done, visiting := map[*Load]bool{}, map[*Load]bool{}
	var all func(load *Load) []string
	all = func(load *Load) []string {
		if done[load] || visiting[load] {
			return load.AllQuery
		}
		visiting[load] = true
		defer func() { delete(visiting, load); done[load] = true }()
		q := slices.Clone(load.Query)
		for _, path := range a.used[load] {
			l := byPkg[path]
			if l == nil {
				continue // not a Layout function, checkUse reports it
			}
			load.Uses = append(load.Uses, l.ID)
			q = append(q, all(l.Load)...)
		}
		slices.Sort(load.Uses)
		load.Uses = slices.Compact(load.Uses)
		slices.Sort(q)
		load.AllQuery = slices.Compact(q)
		return load.AllQuery
	}
	for _, l := range layouts {
		if l.Load != nil {
			all(l.Load)
		}
	}
	for _, r := range routes {
		if r.Load != nil {
			all(r.Load)
		}
	}
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
