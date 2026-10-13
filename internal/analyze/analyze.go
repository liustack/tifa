package analyze

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	tifaPath = "github.com/liustack/tifa"
	corePath = "github.com/liustack/tifa/internal/core"
	ctxType  = "*" + corePath + ".Ctx"
)

// handlers maps the OnXxx functions of route.go to their HTTP methods, in manifest order.
var handlers = []struct{ name, method string }{
	{"OnGet", "GET"}, {"OnPost", "POST"}, {"OnPut", "PUT"}, {"OnPatch", "PATCH"}, {"OnDelete", "DELETE"},
}

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
// Other errors (no app directory, go list failing) are returned as they are.
func Analyze(dir string) (*Manifest, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	a := &analyzer{root: root, dirs: map[string]*dirInfo{}}
	if err := a.scan(); err != nil {
		return nil, err
	}
	if err := a.load(); err != nil {
		return nil, err
	}
	if len(a.errs) > 0 { // the code does not type check, conventions cannot be read reliably
		return nil, a.sortedErrors()
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
	errs   Errors
}

// dirInfo records the convention files found in one directory under app/.
type dirInfo struct {
	id                                     string
	routeGo, routeTSX, layoutGo, layoutTSX bool
}

// scan walks app/ the way the go command does (skipping ".", "_" and testdata directories)
// and records which convention files exist.
func (a *analyzer) scan() error {
	appDir := filepath.Join(a.root, "app")
	if info, err := os.Stat(appDir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s has no app directory", a.root)
	}
	return filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != appDir && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		id := a.id(filepath.Dir(path))
		info := a.dirs[id]
		if info == nil {
			info = &dirInfo{id: id}
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
		default:
			return nil
		}
		a.dirs[id] = info
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

func (a *analyzer) load() error {
	patterns := []string{"./app/..."}
	if _, err := os.Stat(filepath.Join(a.root, "hooks.go")); err == nil {
		patterns = append(patterns, ".")
	}
	a.fset = token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedModule,
		Dir:  a.root,
		Fset: a.fset,
		// The application is built with cgo off, so the same files are analyzed.
		Env: append(os.Environ(), "CGO_ENABLED=0"),
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return err
	}
	a.byDir = map[string]*packages.Package{}
	for _, p := range pkgs {
		// go list compiles each package for export data and reports the compiler output as one
		// error without a position. The type checker reports the same problems with positions.
		positioned := slices.ContainsFunc(p.Errors, func(e packages.Error) bool { return e.Pos != "" })
		for _, e := range p.Errors {
			if e.Pos != "" || !positioned {
				a.errs = append(a.errs, a.packageError(e))
			}
		}
		if len(p.GoFiles) == 0 {
			continue
		}
		if p.Module != nil && a.module == "" {
			a.module = p.Module.Path
		}
		a.pkgs = append(a.pkgs, p)
		id := a.id(filepath.Dir(p.GoFiles[0]))
		a.byDir[id] = p
		if id == "." {
			a.rootPk = p
		}
	}
	return nil
}

// packageError converts a load or type error, whose position is a "file:line:col" string.
func (a *analyzer) packageError(e packages.Error) Error {
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
		return x.Column < y.Column
	})
	return a.errs
}

// route and layout hold what build needs beyond the manifest entries.
type route struct {
	Route
	segs    []segment // empty for Path "/"
	valid   bool      // Path is a literal that parsed, segs and pathPos are set
	pathPos token.Pos
	uses    []paramUse
}

type layout struct {
	Layout
	dir  *dirInfo
	uses []paramUse
}

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
		switch {
		case d.routeGo:
			if d.layoutGo || d.layoutTSX {
				file := "layout.go"
				if !d.layoutGo {
					file = "layout.tsx"
				}
				a.errorInFile(id, file, "route.go and %s are in one directory: a directory holds one route or one Layout", file)
			}
			if r := a.route(d); r != nil {
				routes = append(routes, r)
			}
		case d.routeTSX:
			a.errorInFile(id, "route.tsx", "route.tsx has no route.go next to it to declare its Path")
		case d.layoutGo || d.layoutTSX:
			if l := a.layout(d); l != nil {
				layouts = append(layouts, l)
			}
		}
	}

	a.linkLayouts(routes, layouts)
	a.checkPaths(routes)
	a.checkUses(layouts)

	m := &Manifest{Module: a.module, Hooks: a.hooks()}
	for _, l := range layouts {
		m.Layouts = append(m.Layouts, l.Layout)
	}
	for _, r := range routes {
		m.Routes = append(m.Routes, r.Route)
	}
	return m
}

func (a *analyzer) route(d *dirInfo) *route {
	pkg := a.byDir[d.id]
	if pkg == nil {
		a.errorInFile(d.id, "route.go", "route.go is not part of a package the go command builds")
		return nil
	}
	r := &route{Route: Route{ID: d.id, Package: pkg.PkgPath, Page: d.routeTSX, SSR: true}}
	scope := pkg.Types.Scope()

	if obj := scope.Lookup("Path"); obj == nil {
		a.errorInFile(d.id, "route.go", "route.go must declare const Path")
	} else if lit := a.stringLiteralConst(pkg, obj); lit != nil {
		r.Path = constant.StringVal(pkg.TypesInfo.Types[lit].Value)
		r.pathPos = lit.Pos()
		segs, err := parsePath(r.Path)
		if err != nil {
			a.errorAt(lit.Pos(), "%v", err)
		} else {
			r.segs, r.valid = segs, true
		}
	}

	r.Load, r.uses = a.readLoad(pkg)
	for _, h := range handlers {
		obj := scope.Lookup(h.name)
		if obj == nil {
			continue
		}
		if !a.checkFunc(obj, []string{ctxType}, []string{"error"}, "func(c *tifa.Ctx) error") {
			continue
		}
		if h.name == "OnGet" && d.routeTSX {
			a.errorAt(obj.Pos(), "OnGet is not allowed next to route.tsx: the framework renders GET for a page")
			continue
		}
		r.Methods = append(r.Methods, h.method)
	}
	r.SSR = a.boolConst(pkg, "SSR", true)
	r.Cache = a.boolConst(pkg, "Cache", false)
	return r
}

func (a *analyzer) layout(d *dirInfo) *layout {
	l := &layout{Layout: Layout{ID: d.id, TSX: d.layoutTSX, SSR: true}, dir: d}
	if !d.layoutGo {
		return l
	}
	pkg := a.byDir[d.id]
	if pkg == nil {
		a.errorInFile(d.id, "layout.go", "layout.go is not part of a package the go command builds")
		return nil
	}
	l.Package = pkg.PkgPath
	scope := pkg.Types.Scope()
	for _, name := range []string{"Path", "Cache"} {
		if obj := scope.Lookup(name); obj != nil {
			a.errorAt(obj.Pos(), "a layout does not declare %s, it belongs in route.go", name)
		}
	}
	for _, h := range handlers {
		if obj := scope.Lookup(h.name); obj != nil {
			a.errorAt(obj.Pos(), "a layout does not declare %s, handlers belong in route.go", h.name)
		}
	}
	l.Load, l.uses = a.readLoad(pkg)
	l.SSR = a.boolConst(pkg, "SSR", true)
	return l
}

// stringLiteralConst returns the string literal that declares the constant obj, or reports why it is not one.
func (a *analyzer) stringLiteralConst(pkg *packages.Package, obj types.Object) *ast.BasicLit {
	c, ok := obj.(*types.Const)
	if !ok || c.Val().Kind() != constant.String {
		a.errorAt(obj.Pos(), "%s must be a string constant", obj.Name())
		return nil
	}
	value := a.constValue(pkg, c)
	if lit, ok := ast.Unparen(value).(*ast.BasicLit); ok && lit.Kind == token.STRING {
		return lit
	}
	pos := obj.Pos()
	if value != nil {
		pos = value.Pos()
	}
	a.errorAt(pos, "%s must be a single string literal, the analyzer does not evaluate constant expressions", obj.Name())
	return nil
}

// constValue finds the expression assigned to a package-level constant, nil when it repeats an earlier spec.
func (a *analyzer) constValue(pkg *packages.Package, c *types.Const) ast.Expr {
	for _, f := range pkg.Syntax {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if pkg.TypesInfo.Defs[name] == c && i < len(vs.Values) {
						return vs.Values[i]
					}
				}
			}
		}
	}
	return nil
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

// load_ reads the Load function of a route or Layout package: its result type and the constant
// parameter names passed to c.Param and c.Query in its body.
func (a *analyzer) readLoad(pkg *packages.Package) (*Load, []paramUse) {
	obj := pkg.Types.Scope().Lookup("Load")
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
		a.errorAt(obj.Pos(), "Load must have the signature func(c *tifa.Ctx) (T, error)")
		return nil, nil
	}
	result := sig.Results().At(0).Type()
	load := &Load{Type: typeString(result), Result: result}

	decl := a.funcDecl(pkg, fn)
	resultPos := decl.Type.Results.List[0].Type.Pos()
	for _, p := range checkTS(result) {
		msg := fmt.Sprintf("Load result: %s: %s", p.path, p.msg)
		if p.pos != nil && p.pos.Pos().IsValid() {
			fp := a.fset.Position(p.pos.Pos())
			msg += fmt.Sprintf(" (field declared at %s:%d)", a.relFile(fp.Filename), fp.Line)
		}
		a.errorAt(resultPos, "%s", msg)
	}

	var uses []paramUse
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		method := ctxMethod(pkg.TypesInfo, call)
		if method != "Param" && method != "Query" || len(call.Args) != 1 {
			return true
		}
		tv := pkg.TypesInfo.Types[call.Args[0]]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			a.errorAt(call.Pos(), "the argument of c.%s in Load must be a constant string, the analyzer reads parameter names from it", method)
			return true
		}
		uses = append(uses, paramUse{method: method, name: constant.StringVal(tv.Value), pos: call.Pos()})
		return true
	})
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

// ctxMethod returns the name of the tifa.Ctx method that call invokes, empty for any other call.
func ctxMethod(info *types.Info, call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	s := info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal {
		return ""
	}
	if named, ok := types.Unalias(derefType(s.Recv())).(*types.Named); ok &&
		named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == corePath && named.Obj().Name() == "Ctx" {
		return sel.Sel.Name
	}
	return ""
}

func derefType(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// isAncestor reports whether directory ID outer strictly contains directory ID inner.
func isAncestor(outer, inner string) bool { return strings.HasPrefix(inner, outer+"/") }

// linkLayouts fills Layout chains and scopes, computes each Layout's Path from its scope and
// checks that every path parameter a Layout's Load reads lies in that Path.
func (a *analyzer) linkLayouts(routes []*route, layouts []*layout) {
	for _, l := range layouts {
		for _, p := range layouts { // sorted by ID, so the last match is the closest
			if isAncestor(p.ID, l.ID) {
				l.Parent = p.ID
			}
		}
		// Only pages render inside a Layout, so only their Paths make up its path. Interface
		// routes in the directory stay in Routes but do not shrink the prefix.
		var paths [][]segment
		var scope []*route
		pages := 0
		for _, r := range routes {
			if !isAncestor(l.ID, r.ID) {
				continue
			}
			l.Routes = append(l.Routes, r.ID)
			if !r.Page {
				continue
			}
			pages++
			if r.valid {
				paths = append(paths, r.segs)
				scope = append(scope, r)
			}
		}
		if pages == 0 {
			file := "layout.go"
			if !l.dir.layoutGo {
				file = "layout.tsx"
			}
			a.errorInFile(l.ID, file, "this layout has no pages (route.tsx) in scope, so it has no path")
			continue
		}
		prefix := commonPrefix(paths)
		l.Path = formatPath(prefix)
		for _, u := range l.uses {
			if u.method != "Param" || slices.ContainsFunc(prefix, func(s segment) bool { return s.param == u.name }) {
				continue
			}
			a.errorAt(u.pos, "layout Load reads path parameter %q, which is not in %q, the common prefix of the page Paths in scope: %s",
				u.name, l.Path, a.offendingPaths(scope, u.name))
		}
	}
	for _, r := range routes {
		for _, l := range layouts {
			if isAncestor(l.ID, r.ID) {
				r.Layouts = append(r.Layouts, l.ID)
				if !l.SSR {
					r.SSR = false
				}
			}
		}
	}
}

// offendingPaths names the Paths in scope that lack the parameter, or all of them when every
// Path has it but at different positions.
func (a *analyzer) offendingPaths(scope []*route, name string) string {
	var lacking, all []string
	for _, r := range scope {
		desc := fmt.Sprintf("%q (%s)", r.Path, a.posString(r.pathPos))
		all = append(all, desc)
		if !slices.ContainsFunc(r.segs, func(s segment) bool { return s.param == name }) {
			lacking = append(lacking, desc)
		}
	}
	if len(lacking) > 0 {
		return "missing from " + strings.Join(lacking, ", ")
	}
	return "it sits at different positions in " + strings.Join(all, ", ")
}

func (a *analyzer) posString(pos token.Pos) string {
	p := a.fset.Position(pos)
	return fmt.Sprintf("%s:%d", a.relFile(p.Filename), p.Line)
}

// checkPaths reports c.Param names that are not in the route's own Path, and every pair of
// Paths that ServeMux would refuse to register together: a duplicate when they match the same
// URLs, a conflict when they share some URLs and neither is more specific. Each pair is reported
// once, at the later route.
func (a *analyzer) checkPaths(routes []*route) {
	var checked []*route
	for _, r := range routes {
		if !r.valid {
			continue
		}
		for _, u := range r.uses {
			if u.method == "Param" && !slices.ContainsFunc(r.segs, func(s segment) bool { return s.param == u.name }) {
				a.errorAt(u.pos, "c.Param(%q) reads a parameter that is not in Path %q", u.name, r.Path)
			}
		}
		for _, other := range checked {
			switch comparePaths(muxSegments(other.segs), muxSegments(r.segs)) {
			case equivalent:
				a.errorAt(r.pathPos, "Path %q duplicates Path %q of %s", r.Path, other.Path, a.posString(other.pathPos))
			case overlaps:
				a.errorAt(r.pathPos, "Path %q conflicts with Path %q of %s: both match some URLs and neither is more specific",
					r.Path, other.Path, a.posString(other.pathPos))
			}
		}
		checked = append(checked, r)
	}
}

// checkUses finds every call of tifa.Use in the loaded packages. Its function argument must be
// a package-level function, because the runtime tells Loads apart by code address and closures
// of one literal or method values of one method would share results. It must also be the Load
// of a Layout whose directory strictly contains the calling package.
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
				call, ok := n.(*ast.CallExpr)
				if !ok || !isTifaUse(pkg.TypesInfo, call) || len(call.Args) != 2 {
					return true
				}
				arg := ast.Unparen(call.Args[1])
				text := types.ExprString(arg)
				const need = "the function passed to tifa.Use must be a package-level function"
				var fn *types.Func
				switch e := arg.(type) {
				case *ast.FuncLit:
					a.errorAt(arg.Pos(), "%s, got a closure: tifa.Use tells Loads apart by code address, so closures of one literal would share results", need)
					return true
				case *ast.SelectorExpr:
					if pkg.TypesInfo.Selections[e] != nil {
						a.errorAt(arg.Pos(), "%s, got the method value %s: method values of one method share a code address and would share results", need, text)
						return true
					}
					fn, _ = pkg.TypesInfo.Uses[e.Sel].(*types.Func)
				case *ast.Ident:
					fn, _ = pkg.TypesInfo.Uses[e].(*types.Func)
				}
				if fn == nil || fn.Signature().Recv() != nil || fn.Pkg() == nil || fn.Parent() != fn.Pkg().Scope() {
					a.errorAt(arg.Pos(), "%s, got %s", need, text)
					return true
				}
				l := layoutByPkg[fn.Pkg().Path()]
				if l == nil || fn.Name() != "Load" {
					a.errorAt(arg.Pos(), "tifa.Use(c, %s): %s is not the Load of a layout", text, text)
					return true
				}
				if !isAncestor(l.ID, caller) {
					a.errorAt(arg.Pos(), "tifa.Use(c, %s): the layout %s is not an ancestor of %s", text, l.ID, caller)
				}
				return true
			})
		}
	}
}

func isTifaUse(info *types.Info, call *ast.CallExpr) bool {
	fun := ast.Unparen(call.Fun)
	switch e := fun.(type) { // tifa.Use[T](...) carries explicit type arguments
	case *ast.IndexExpr:
		fun = e.X
	case *ast.IndexListExpr:
		fun = e.X
	}
	var id *ast.Ident
	switch e := fun.(type) {
	case *ast.SelectorExpr:
		id = e.Sel
	case *ast.Ident:
		id = e
	default:
		return false
	}
	fn, ok := info.Uses[id].(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == tifaPath && fn.Name() == "Use"
}

// hooks lists the hooks that the root package defines and checks their signatures.
func (a *analyzer) hooks() []string {
	if a.rootPk == nil {
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
