package analyze

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// fixHint tells the user how to change a call the analyzer cannot follow.
const fixHint = `read the values with c.Param("name") and c.Query("name") in Load, or in a function the analyzer follows, and pass the values instead, or pass c.Context() when only the request context is needed`

// loadWalk collects what one Load reads: the c.Param and c.Query names and the tifa.Use calls in
// its body and in every function it passes the Ctx to, followed through any number of calls
// inside the module. Each Load gets its own walk, so a helper shared by several Loads counts for
// each of them.
type loadWalk struct {
	a       *analyzer
	owner   string // directory ID of the route or Layout that declares the Load
	self    string // import path of the package of a Layout function, "" for a page's Load
	load    *Load
	uses    []paramUse
	visited map[string]bool // followed functions by full name, against recursion
}

// useCall is a tifa.Use call in a helper, checked against the directory of the Load that reaches it.
type useCall struct {
	info   *types.Info
	call   *ast.CallExpr
	caller string
}

// walkLoad walks the function decl of pkg that loads the data of the route or Layout in the
// directory owner. layout reports whether it is a Layout function.
func (a *analyzer) walkLoad(pkg *packages.Package, owner string, layout bool, load *Load, decl *ast.FuncDecl) []paramUse {
	w := &loadWalk{a: a, owner: owner, load: load, visited: map[string]bool{}}
	if layout {
		w.self = pkg.PkgPath
	}
	a.checkCtxFlow(pkg.TypesInfo, decl.Body)
	w.body(pkg.TypesInfo, decl.Body, false)
	return w.uses
}

func (w *loadWalk) body(info *types.Info, body *ast.BlockStmt, helper bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GoStmt:
			if mentionsCtx(info, n.Call) {
				w.a.errorAt(n.Pos(), "a go statement hands the Ctx to another goroutine, which the analyzer does not follow: %s", fixHint)
				return false
			}
		case *ast.CallExpr:
			w.call(info, n, helper)
		}
		return true
	})
}

func (w *loadWalk) call(info *types.Info, call *ast.CallExpr, helper bool) {
	a := w.a
	if isTifaUse(info, call) {
		if len(call.Args) != 2 {
			return
		}
		// checkUses reports arguments that are not the Layout function of a wrapping Layout.
		if fn := packageFunc(info, call.Args[1]); fn != nil && fn.Name() == "Layout" {
			if fn.Pkg().Path() == w.self {
				a.errorAt(call.Args[1].Pos(), "tifa.Use(c, Layout) is reached from the Layout function of %s itself, which would wait for its own result", w.owner)
				return
			}
			a.used[w.load] = append(a.used[w.load], fn.Pkg().Path())
		}
		if helper {
			a.helperUses = append(a.helperUses, useCall{info: info, call: call, caller: w.owner})
		}
		return
	}
	if method := ctxMethod(info, call); method == "Param" || method == "Query" {
		if len(call.Args) != 1 {
			return
		}
		tv := info.Types[call.Args[0]]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			a.errorAt(call.Pos(), "the argument of c.%s must be a constant string, the analyzer reads parameter names from it", method)
			return
		}
		w.uses = append(w.uses, paramUse{method: method, name: constant.StringVal(tv.Value), pos: call.Pos()})
		return
	}
	if fn, ok := typeutil.Callee(info, call).(*types.Func); ok && rawAccess(fn) {
		a.errorAt(call.Pos(), "c.%s gives the raw request or response, where the analyzer cannot see which parameters are read: in Load and the functions it calls, use c.Param, c.Query, c.Header, c.Cookie or c.Context() instead. c.Request and c.Writer are for the GET, POST, PUT, PATCH and DELETE handlers and OnRequest", fn.Name())
		return
	}
	if info.Types[call.Fun].IsType() || !callCarriesCtx(info, call) {
		return
	}
	switch callee := typeutil.Callee(info, call).(type) {
	case *types.Builtin:
	case *types.Func:
		if callee.Pkg() != nil && isFrameworkPath(callee.Pkg().Path()) {
			return
		}
		if recv := callee.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
			a.errorAt(call.Pos(), "%s is an interface method that receives the Ctx, the analyzer cannot tell which implementation runs: %s", methodName(callee), fixHint)
			return
		}
		key := callee.Origin().FullName()
		if src, ok := a.funcs[key]; ok {
			w.follow(key, src)
			return
		}
		if callee.Pkg() != nil && a.modulePkgs[callee.Pkg().Path()] {
			return // a module package that does not type check, its errors are reported
		}
		a.errorAt(call.Pos(), "%s is outside this module and receives the Ctx, the analyzer cannot read its source: %s", callee.FullName(), fixHint)
	default: // a variable, parameter or field of function type, or a call result
		if _, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
			return // the literal's body is inspected as part of the enclosing body
		}
		a.errorAt(call.Pos(), "%s is a function value that receives the Ctx, the analyzer cannot tell which function runs: call a named function, or %s", types.ExprString(call.Fun), fixHint)
	}
}

// follow walks the body of a function in the module that receives the Ctx. The checks that do
// not depend on the Load run once per function: Param and Query must be called directly there
// too, and the Ctx must not be converted to an interface with these methods.
func (w *loadWalk) follow(key string, src funcSource) {
	if w.visited[key] || src.decl.Body == nil {
		return
	}
	w.visited[key] = true
	a := w.a
	info := src.pkg.TypesInfo
	if !a.checked[key] {
		a.checked[key] = true
		a.checkCtxFlow(info, src.decl.Body)
		if !a.appPkgs[src.pkg.PkgPath] { // checkDirectCalls covers the application packages whole
			a.checkDirectCallsIn(info, src.decl)
		}
	}
	w.body(info, src.decl.Body, true)
}

// callCarriesCtx reports whether a call passes the Ctx, as an argument or as the receiver of a
// method, by itself or inside a value that holds it.
func callCarriesCtx(info *types.Info, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if containsCtx(info.TypeOf(arg)) {
			return true
		}
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if s := info.Selections[sel]; s != nil && s.Kind() == types.MethodVal {
			return containsCtx(info.TypeOf(sel.X))
		}
	}
	return false
}

// mentionsCtx reports whether any variable inside n holds the Ctx.
func mentionsCtx(info *types.Info, n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types.Var); ok && containsCtx(v.Type()) {
				found = true
			}
		}
		return !found
	})
	return found
}

// containsCtx reports whether a value of type t holds a tifa.Ctx: the Ctx itself, a pointer to it,
// or a struct, array, slice, map or channel with one inside.
func containsCtx(t types.Type) bool {
	return containsCtxSeen(t, map[types.Type]bool{})
}

func containsCtxSeen(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		if obj := t.Obj(); obj.Pkg() != nil && obj.Pkg().Path() == corePath && obj.Name() == "Ctx" {
			return true
		}
		return containsCtxSeen(t.Underlying(), seen)
	case *types.Pointer:
		return containsCtxSeen(t.Elem(), seen)
	case *types.Struct:
		for i := range t.NumFields() {
			if containsCtxSeen(t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Array:
		return containsCtxSeen(t.Elem(), seen)
	case *types.Slice:
		return containsCtxSeen(t.Elem(), seen)
	case *types.Map:
		return containsCtxSeen(t.Key(), seen) || containsCtxSeen(t.Elem(), seen)
	case *types.Chan:
		return containsCtxSeen(t.Elem(), seen)
	case *types.Tuple:
		for v := range t.Variables() {
			if containsCtxSeen(v.Type(), seen) {
				return true
			}
		}
	}
	return false
}

// isFrameworkPath reports whether an import path belongs to tifa itself, whose functions read
// no parameters on the application's behalf.
func isFrameworkPath(path string) bool {
	return path == tifaPath || strings.HasPrefix(path, tifaPath+"/")
}

// methodName names an interface method by its interface: reader.Read.
func methodName(fn *types.Func) string {
	if named, ok := types.Unalias(derefType(fn.Signature().Recv().Type())).(*types.Named); ok {
		return named.Obj().Name() + "." + fn.Name()
	}
	return fn.Name()
}

// rawAccess reports whether fn is Ctx.Request or Ctx.Writer, which hand out the http.Request and
// the http.ResponseWriter.
func rawAccess(fn *types.Func) bool {
	recv := fn.Signature().Recv()
	if recv == nil || fn.Name() != "Request" && fn.Name() != "Writer" {
		return false
	}
	named, ok := types.Unalias(derefType(recv.Type())).(*types.Named)
	return ok && isCtxNamed(named)
}
