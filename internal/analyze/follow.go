package analyze

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// loadWalk collects what one Load reads: the c.Param names and the tifa.Use calls in its body
// and in every function of the module it passes the Ctx to, as an argument or as the receiver of a
// method, followed through any number of calls. The walk is best effort: it stops where it cannot
// see which function runs (an interface method, a function value, a function outside the module)
// and says nothing about it, as the Ctx may go anywhere a value can. What it reaches is checked,
// what it does not is the app's own, as it is for an http.Handler. Each Load gets its own walk, so
// a helper shared by several Loads counts for each of them.
type loadWalk struct {
	a        *analyzer
	owner    string // directory ID of the route or Layout that declares the Load
	self     string // import path of the package of a Layout function, "" for a page's Load
	load     *Load
	validate bool // the walk of a Validate method, which only reads the request (see refusedInValidate)
	layout   bool // the walk of a Layout function
	uses     []paramUse
	visited  map[string]bool // followed functions by full name, against recursion
	unrun    []*ast.FuncLit  // closures of the body being walked that are declared and not called on the spot
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
	w := &loadWalk{a: a, owner: owner, load: load, visited: map[string]bool{}, layout: layout}
	if layout {
		w.self = pkg.PkgPath
	}
	w.body(pkg.TypesInfo, decl.Body, false)
	return w.uses
}

func (w *loadWalk) body(info *types.Info, body *ast.BlockStmt, helper bool) {
	saved := w.unrun
	defer func() { w.unrun = saved }()
	w.unrun = unrunClosures(body)
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			w.call(info, call, helper)
		}
		return true
	})
}

func (w *loadWalk) call(info *types.Info, call *ast.CallExpr, helper bool) {
	a := w.a
	if isTifaUse(info, call) {
		if w.validate {
			a.errorAt(call.Pos(), "tifa.Use loads the data of a Layout for a page: it has no place in Validate or the functions it calls, which only check the input")
			return
		}
		if len(call.Args) != 2 {
			return
		}
		// checkUses reports arguments that are not the Layout function of a wrapping Layout.
		if fn := packageFunc(info, call.Args[1]); fn != nil && fn.Name() == "Layout" && fn.Pkg().Path() == w.self {
			a.errorAt(call.Args[1].Pos(), "tifa.Use(c, Layout) is reached from the Layout function of %s itself, which would wait for its own result", w.owner)
			return
		}
		if helper {
			a.helperUses = append(a.helperUses, useCall{info: info, call: call, caller: w.owner})
		}
		return
	}
	if method := ctxMethod(info, call); method != "" {
		if method != "Param" && !w.validate {
			what := "Load"
			if w.layout {
				what = "Layout"
			}
			a.errorAt(call.Pos(), "c.%s cannot be used in Load, Layout or the functions they call: they take their query parameters as a second parameter, which the build and the browser can read. Declare the parameters in a struct and take it as the second parameter, for example type Filter struct { Sort string `json:\"sort\"` } and func %s(c *tifa.Ctx, q Filter) (T, error), then read q.Sort. c.%s is for method handlers, Validate and OnRequest", method, what, method)
			return
		}
		// A name that is not a constant is read when the page runs, and the build checks only the
		// names it can see.
		if method != "Param" || len(call.Args) != 1 {
			return
		}
		if tv := info.Types[call.Args[0]]; tv.Value != nil && tv.Value.Kind() == constant.String {
			w.uses = append(w.uses, paramUse{method: method, name: constant.StringVal(tv.Value), pos: call.Pos()})
		}
		return
	}
	if fn, ok := typeutil.Callee(info, call).(*types.Func); ok {
		if w.validate {
			if why := refusedInValidate(fn); why != "" {
				a.errorAt(call.Pos(), "%s", why)
				return
			}
		} else {
			if what := identityCall(fn); what != "" {
				a.identity[w.load] = append(a.identity[w.load], identityRead{pos: call.Pos(), what: what})
			}
			if why := refusedInLayout(fn); why != "" && w.layout {
				a.errorAt(call.Pos(), "%s", why)
				return
			}
			if why := refusedInLoad(fn); why != "" {
				a.errorAt(call.Pos(), "%s", why)
				return
			}
			if isSessionWrite(fn) && !w.inUnrunClosure(call.Pos()) {
				a.errorAt(call.Pos(), "tifa.%s cannot run in Load, Layout or the functions they call: it changes the cookie of the response, and a page load or a data request must not sign anyone in or out. Call it in OnRequest before next, or in a method handler (POST, PUT, PATCH, DELETE)", fn.Name())
				return
			}
		}
	}
	if info.Types[call.Fun].IsType() || !callCarriesCtx(info, call) {
		return
	}
	// A builtin, a function value, an interface method and a function outside the module are not
	// followed: the build cannot read which code runs. A function literal called on the spot is
	// inspected as part of the body around it.
	callee, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok || callee.Pkg() != nil && a.isFramework(callee.Pkg().Path()) {
		return
	}
	key := callee.Origin().FullName()
	if src, ok := a.funcs[key]; ok {
		w.follow(key, src)
	}
}

// follow walks the body of a function in the module that receives the Ctx.
func (w *loadWalk) follow(key string, src funcSource) {
	if w.visited[key] || src.decl.Body == nil {
		return
	}
	w.visited[key] = true
	w.body(src.pkg.TypesInfo, src.decl.Body, true)
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

func isCtxNamed(n *types.Named) bool {
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == corePath && obj.Name() == "Ctx"
}

// shortType writes a type with package names instead of import paths.
func shortType(t types.Type) string {
	if t == nil {
		return "no parameter"
	}
	return types.TypeString(t, shortQualifier)
}

// isFrameworkPath reports whether an import path belongs to tifa itself, whose functions read
// no parameters on the application's behalf.
func (a *analyzer) isFramework(path string) bool {
	return isFrameworkPath(path) && !a.modulePkgs[path]
}

func isFrameworkPath(path string) bool {
	return path == tifaPath || strings.HasPrefix(path, tifaPath+"/")
}

// refusedInLoad returns why fn may not run in Load, Layout and the functions they call, and an
// empty string when it may. The refused functions are the methods of the Ctx that read the
// request where the analyzer cannot see which parameters are read, so the page would not load
// again when they change, and those that write the response, which the framework writes for the
// page.
func refusedInLoad(fn *types.Func) string {
	recv := fn.Signature().Recv()
	if recv == nil {
		return ""
	}
	if named, ok := types.Unalias(derefType(recv.Type())).(*types.Named); !ok || !isCtxNamed(named) {
		return ""
	}
	switch fn.Name() {
	case "Request", "Writer":
		return "c." + fn.Name() + " gives the raw request or response, where the analyzer cannot see which path parameters are read: in Load and the functions it calls, use c.Param, c.Header, c.Cookie, c.Path (the path of the page) or c.Context() instead, and take the query parameters as the second parameter of Load. c.Request and c.Writer are for the GET, POST, PUT, PATCH and DELETE handlers and OnRequest"
	case "BindQuery":
		return `c.BindQuery decodes the query string in a method handler, where the response is the handler's own: Load and Layout take their query parameters as the second parameter, for example type Filter struct { Page int ` + "`json:\"page\"`" + ` } and func Load(c *tifa.Ctx, q Filter) (T, error)`
	case "Bind":
		return `c.Bind reads the request body, which a page load and the data request of a navigation do not have: in Load and the functions it calls, take the query parameters as the second parameter, func Load(c *tifa.Ctx, q Filter) (T, error). c.Bind is for the POST, PUT, PATCH and DELETE handlers`
	case "JSON", "Text", "NoContent":
		return "c." + fn.Name() + " writes the response, which tifa writes for a page from what its Load and Layout functions return: in Load, Layout and the functions they call, return the data, an error such as tifa.Error{Status: 404, Message: \"no such order\"}, or tifa.Redirect(url, 302). c." + fn.Name() + " is for method handlers and OnRequest"
	}
	return ""
}

// unrunClosures returns the function literals in body that nothing calls where they are written.
// What such a closure does depends on who runs it, which the analyzer does not follow, so a
// sign-in or sign-out inside it is not a violation of the Load.
func unrunClosures(body *ast.BlockStmt) []*ast.FuncLit {
	called := map[*ast.FuncLit]bool{}
	var lits []*ast.FuncLit
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if lit, ok := ast.Unparen(n.Fun).(*ast.FuncLit); ok {
				called[lit] = true
			}
		case *ast.FuncLit:
			lits = append(lits, n)
		}
		return true
	})
	return slices.DeleteFunc(lits, func(l *ast.FuncLit) bool { return called[l] })
}

// inUnrunClosure reports whether pos is inside a closure of the body that is not called where it is written.
func (w *loadWalk) inUnrunClosure(pos token.Pos) bool {
	return slices.ContainsFunc(w.unrun, func(l *ast.FuncLit) bool { return l.Pos() <= pos && pos < l.End() })
}

// identityRead is a call in the body of a Load, or of a function it calls, that reads who the
// visitor is: c.Cookie, c.Header or tifa.Session. A page with Cache answers every visitor alike, so
// these reads there always see an anonymous request.
type identityRead struct {
	pos  token.Pos
	what string // as the user writes it: c.Cookie, c.Header, tifa.Session
}

// identityCall returns the name of fn as the user writes it when it reads the cookie, a header or
// the session of the request, and an empty string for any other function.
func identityCall(fn *types.Func) string {
	if recv := fn.Signature().Recv(); recv != nil {
		if named, ok := types.Unalias(derefType(recv.Type())).(*types.Named); ok && isCtxNamed(named) && (fn.Name() == "Cookie" || fn.Name() == "Header") {
			return "c." + fn.Name()
		}
		return ""
	}
	if fn.Pkg() != nil && fn.Pkg().Path() == tifaPath && fn.Name() == "Session" {
		return "tifa.Session"
	}
	return ""
}

// refusedInValidate returns why fn may not run in a Validate method of an input and the functions
// it passes the Ctx to, and an empty string when it may. Validate checks the input, then the
// handler runs: it may read the request (Param, Query, QueryValues, Header, Cookie, Language,
// Path, Pattern, IsDataRequest, Context, Request and tifa.Session) and answers with an error. It may not
// write the response, read the body again, change the session or touch the Layout data.
func refusedInValidate(fn *types.Func) string {
	if isSessionWrite(fn) {
		return "tifa." + fn.Name() + " cannot run in Validate or the functions it calls: Validate only checks the input. Call it in the method handler, after the input passed"
	}
	recv := fn.Signature().Recv()
	if recv == nil {
		return ""
	}
	if named, ok := types.Unalias(derefType(recv.Type())).(*types.Named); !ok || !isCtxNamed(named) {
		return ""
	}
	switch fn.Name() {
	case "JSON", "Text", "NoContent", "SetHeader", "SetCookie", "Writer", "SetAlternates":
		return "c." + fn.Name() + " changes the response, which cannot be done in Validate or the functions it calls: Validate only checks the input, and what it returns is the answer. Return an error, or tifa.Fields for single fields, and write the response in the method handler"
	case "Bind", "BindQuery", "SetBodyLimit", "SetReadTimeout":
		return "c." + fn.Name() + " reads or limits the request body, which is already decoded when Validate runs: use the fields of the input"
	}
	return ""
}

// refusedInLayout returns why fn may not run in a Layout function and the functions it calls, and
// an empty string when it may. The data of a Layout is shared by every page below it and is not
// loaded again when the visitor goes from one of them to another, so it cannot hold anything of
// the one page that happened to load it first.
func refusedInLayout(fn *types.Func) string {
	recv := fn.Signature().Recv()
	if recv == nil {
		return ""
	}
	if named, ok := types.Unalias(derefType(recv.Type())).(*types.Named); !ok || !isCtxNamed(named) {
		return ""
	}
	if fn.Name() == "Path" {
		return "c.Path is the path of the page, which a Layout does not have: its data is shared by every page below it and is not loaded again when the visitor goes from one of them to another. Call c.Path in the Load of a page, or in a method handler"
	}
	return ""
}
