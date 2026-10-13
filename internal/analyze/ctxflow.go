package analyze

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// ctxRule ends every error about where the Ctx went.
const ctxRule = "the Ctx may only be the receiver of a method call, an argument of a function in this module or in tifa whose parameter has the same type, a field of the same type in a struct literal, or a new local variable of the same type: call its methods directly or pass it to an ordinary function"

// checkCtxFlow keeps the Ctx where the analyzer can see every call made on it. Inside body,
// closures included, each expression whose type holds the Ctx must have a type that carries it
// (see carriesCtx) and stand in one of these places:
//
//   - the receiver of a method call written as x.M(...), or the operand of a field selection
//   - an argument of a call to a non-generic function of the module, or of tifa, whose parameter
//     has the identical type, also a function literal called on the spot
//   - a field value of the identical type in a composite literal of a type that carries the Ctx
//   - the value of a new local variable of the identical type, declared with := or var
//   - the operand of &, when the pointer carries the Ctx too
//
// Anything else is an error: conversions, interfaces, other variables, assignments with =,
// several values assigned together, returns, sends, elements of slices, maps and arrays,
// package-level variables, defer statements. Calls of interface methods, function values and
// functions outside the module, and go statements, are reported by loadWalk. When an expression
// and one inside it are both wrong, only the inner one is reported.
func (a *analyzer) checkCtxFlow(info *types.Info, body *ast.BlockStmt) {
	type problem struct {
		e   ast.Expr
		msg string
	}
	var problems []problem
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if _, ok := n.(*ast.GoStmt); ok {
			return false // loadWalk reports a go statement that mentions the Ctx
		}
		parents := stack
		stack = append(stack, n)
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		tv, ok := info.Types[e]
		if !ok || !tv.IsValue() || !containsCtx(tv.Type) {
			return true
		}
		if msg := a.ctxUse(info, e, tv.Type, parents); msg != "" {
			problems = append(problems, problem{e, msg})
		}
		return true
	})
	for _, p := range problems {
		inner := false
		for _, q := range problems {
			if q.e != p.e && q.e.Pos() >= p.e.Pos() && q.e.End() <= p.e.End() {
				inner = true
				break
			}
		}
		if !inner {
			a.errorAt(p.e.Pos(), "%s: %s", p.msg, ctxRule)
		}
	}
}

// ctxUse returns why expression e of type t, whose ancestors are parents, may not hold the Ctx
// there, or an empty string when it may.
func (a *analyzer) ctxUse(info *types.Info, e ast.Expr, t types.Type, parents []ast.Node) string {
	text := types.ExprString(e)
	if as, ok := parentSkippingParens(e, parents).(*ast.AssignStmt); ok && isTarget(as, e, parents) {
		return "" // the assigned value is checked
	}
	if _, ok := t.(*types.Tuple); ok {
		return fmt.Sprintf("%s returns several values, one of them holding the Ctx", text)
	}
	if !a.carriesCtx(t) {
		return fmt.Sprintf("%s has type %s, which may not hold the Ctx", text, shortType(t))
	}
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := info.Uses[id].(*types.Var); ok && v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			return fmt.Sprintf("%s is a package-level variable", text)
		}
	}
	// Parentheses change nothing.
	var child ast.Node = e
	i := len(parents) - 1
	for i >= 0 {
		if _, ok := parents[i].(*ast.ParenExpr); !ok {
			break
		}
		child = parents[i]
		i--
	}
	if i < 0 {
		return ""
	}
	switch p := parents[i].(type) {
	case *ast.SelectorExpr:
		sel := info.Selections[p]
		if sel == nil || sel.Kind() == types.FieldVal {
			return "" // the field is checked as an expression of its own
		}
		if sel.Kind() == types.MethodVal {
			if call := enclosingCall(parents[:i], p); call != nil {
				if _, ok := parents[indexOf(parents, call)-1].(*ast.DeferStmt); ok {
					return fmt.Sprintf("%s is used in a defer statement", text)
				}
				return ""
			}
			if paramMethod(sel.Obj()) != "" {
				return "" // checkDirectCalls reports c.Param and c.Query taken as values
			}
			return fmt.Sprintf("%s has its method %s taken as a value", text, sel.Obj().Name())
		}
	case *ast.CallExpr:
		if _, ok := parents[i-1].(*ast.DeferStmt); ok {
			return fmt.Sprintf("%s is used in a defer statement", text)
		}
		return a.ctxArg(info, p, child, t, text)
	case *ast.KeyValueExpr:
		if child == p.Key {
			return fmt.Sprintf("%s is a map key", text)
		}
		if lit, ok := parents[i-1].(*ast.CompositeLit); ok {
			return a.ctxElem(info, lit, p, t, text)
		}
	case *ast.CompositeLit:
		return a.ctxElem(info, p, child, t, text)
	case *ast.AssignStmt:
		if len(p.Lhs) != len(p.Rhs) {
			return fmt.Sprintf("%s is one of several values assigned together", text)
		}
		if p.Tok != token.DEFINE {
			return fmt.Sprintf("%s is assigned to an existing variable", text)
		}
		for j, rhs := range p.Rhs {
			if rhs != child {
				continue
			}
			id, ok := p.Lhs[j].(*ast.Ident)
			if !ok || id.Name == "_" {
				return fmt.Sprintf("%s is assigned to an existing variable", text)
			}
			v, ok := info.Defs[id].(*types.Var)
			if !ok {
				return fmt.Sprintf("%s is assigned to an existing variable", text)
			}
			return newLocal(v, t, text)
		}
	case *ast.ValueSpec:
		if len(p.Names) != len(p.Values) {
			return fmt.Sprintf("%s is one of several values assigned together", text)
		}
		for j, v := range p.Values {
			if v == child {
				if p.Names[j].Name == "_" {
					return fmt.Sprintf("%s is assigned to an existing variable", text)
				}
				return newLocal(info.Defs[p.Names[j]].(*types.Var), t, text)
			}
		}
	case *ast.UnaryExpr:
		if p.Op == token.AND && a.carriesCtx(info.TypeOf(p)) {
			return "" // the pointer is checked as an expression of its own
		}
		if p.Op == token.AND {
			return fmt.Sprintf("%s has its address taken", text)
		}
	case *ast.ReturnStmt:
		return fmt.Sprintf("%s is returned", text)
	case *ast.SendStmt:
		return fmt.Sprintf("%s is sent on a channel", text)
	case *ast.RangeStmt:
		return fmt.Sprintf("%s is ranged over", text)
	case *ast.StarExpr:
		return fmt.Sprintf("%s is dereferenced", text)
	}
	return fmt.Sprintf("%s is used in an expression the analyzer does not follow", text)
}

// newLocal accepts a new local variable v holding a value of type t when the types are identical.
func newLocal(v *types.Var, t types.Type, text string) string {
	if types.Identical(v.Type(), t) {
		return ""
	}
	return fmt.Sprintf("%s is assigned to a variable of type %s", text, shortType(v.Type()))
}

// ctxArg checks a value that holds the Ctx and is an argument of call.
func (a *analyzer) ctxArg(info *types.Info, call *ast.CallExpr, arg ast.Node, t types.Type, text string) string {
	if tv := info.Types[call.Fun]; tv.IsType() {
		return fmt.Sprintf("%s is converted to %s", text, shortType(tv.Type))
	}
	var name string
	switch callee := typeutil.Callee(info, call).(type) {
	case *types.Builtin:
		return fmt.Sprintf("%s is passed to the builtin %s", text, callee.Name())
	case *types.Func:
		framework := callee.Pkg() != nil && isFrameworkPath(callee.Pkg().Path())
		if !framework {
			if _, ok := a.funcs[callee.Origin().FullName()]; !ok {
				return "" // loadWalk reports interface methods and functions outside the module
			}
			sig := callee.Origin().Signature()
			if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
				return fmt.Sprintf("%s is passed to the generic function %s", text, funcName(callee))
			}
		}
		name = funcName(callee)
	case nil:
		if _, ok := ast.Unparen(call.Fun).(*ast.FuncLit); !ok {
			return "" // loadWalk reports function values
		}
		name = "a function literal"
	default:
		return "" // loadWalk reports function values
	}
	sig, ok := info.TypeOf(call.Fun).Underlying().(*types.Signature)
	if !ok {
		return ""
	}
	for i, x := range call.Args {
		if x != arg {
			continue
		}
		param := paramType(sig, i, call.Ellipsis.IsValid())
		if param == nil || !types.Identical(param, t) {
			return fmt.Sprintf("%s is passed as %s to %s", text, shortType(param), name)
		}
	}
	return ""
}

// paramType returns the type a call's i-th argument is assigned to.
func paramType(sig *types.Signature, i int, ellipsis bool) types.Type {
	params := sig.Params()
	switch {
	case sig.Variadic() && i >= params.Len()-1:
		last := params.At(params.Len() - 1).Type()
		if ellipsis {
			return last
		}
		return last.Underlying().(*types.Slice).Elem()
	case i < params.Len():
		return params.At(i).Type()
	}
	return nil
}

// ctxElem checks a value that holds the Ctx and is an element of a composite literal: only a field
// of the identical type in a literal of a type that carries the Ctx is allowed.
func (a *analyzer) ctxElem(info *types.Info, lit *ast.CompositeLit, elem ast.Node, t types.Type, text string) string {
	litType := info.TypeOf(lit)
	st, ok := litType.Underlying().(*types.Struct)
	if !ok || !a.carriesCtx(litType) {
		return fmt.Sprintf("%s is an element of %s", text, shortType(litType))
	}
	var field *types.Var
	if kv, ok := elem.(*ast.KeyValueExpr); ok {
		field, _ = info.Uses[kv.Key.(*ast.Ident)].(*types.Var)
	} else {
		for i, x := range lit.Elts {
			if x == elem {
				field = st.Field(i)
			}
		}
	}
	if field == nil || !types.Identical(field.Type(), t) {
		ft := "another type"
		name := "?"
		if field != nil {
			ft, name = shortType(field.Type()), field.Name()
		}
		return fmt.Sprintf("%s is stored in the field %s of type %s", text, name, ft)
	}
	return ""
}

// carriesCtx reports whether values of type t may hold the Ctx: *tifa.Ctx, tifa.Ctx, a
// non-generic struct type declared in the module whose fields hold the Ctx only through types
// that carry it, and a pointer to such a struct.
func (a *analyzer) carriesCtx(t types.Type) bool {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	if isCtxNamed(named) {
		return true
	}
	if done, ok := a.carrying[named]; ok {
		return done
	}
	a.carrying[named] = true // a recursive reference counts as carrying while the struct is checked
	ok = a.ctxStruct(named)
	a.carrying[named] = ok
	return ok
}

func (a *analyzer) ctxStruct(named *types.Named) bool {
	obj := named.Obj()
	if obj.Pkg() == nil || !a.modulePkgs[obj.Pkg().Path()] || named.TypeParams().Len() > 0 || named.TypeArgs().Len() > 0 {
		return false
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	carries := false
	for i := range st.NumFields() {
		ft := st.Field(i).Type()
		if !containsCtx(ft) {
			continue
		}
		if !a.carriesCtx(ft) {
			return false
		}
		carries = true
	}
	return carries
}

func isCtxNamed(n *types.Named) bool {
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == corePath && obj.Name() == "Ctx"
}

// enclosingCall returns the call whose function is sel, looking through parentheses, or nil.
func enclosingCall(parents []ast.Node, sel *ast.SelectorExpr) *ast.CallExpr {
	var child ast.Node = sel
	for i := len(parents) - 1; i >= 0; i-- {
		switch p := parents[i].(type) {
		case *ast.ParenExpr:
			child = p
			continue
		case *ast.CallExpr:
			if p.Fun == child {
				return p
			}
		}
		return nil
	}
	return nil
}

func indexOf(nodes []ast.Node, n ast.Node) int {
	for i, x := range nodes {
		if x == n {
			return i
		}
	}
	return -1
}

// funcName names a function as the user writes it: pkg.F or pkg.T.M.
func funcName(fn *types.Func) string {
	name := fn.Name()
	if recv := fn.Signature().Recv(); recv != nil {
		if n, ok := types.Unalias(derefType(recv.Type())).(*types.Named); ok {
			name = n.Obj().Name() + "." + name
		}
	}
	if fn.Pkg() != nil {
		name = fn.Pkg().Name() + "." + name
	}
	return name
}

// shortType writes a type with package names instead of import paths.
func shortType(t types.Type) string {
	if t == nil {
		return "no parameter"
	}
	return types.TypeString(t, shortQualifier)
}

// parentSkippingParens returns the nearest ancestor of e that is not a parenthesis.
func parentSkippingParens(e ast.Expr, parents []ast.Node) ast.Node {
	for i := len(parents) - 1; i >= 0; i-- {
		if _, ok := parents[i].(*ast.ParenExpr); !ok {
			return parents[i]
		}
	}
	return nil
}

// isTarget reports whether e, possibly in parentheses, is on the left of as.
func isTarget(as *ast.AssignStmt, e ast.Expr, parents []ast.Node) bool {
	var child ast.Node = e
	for i := len(parents) - 1; i >= 0 && parents[i] != as; i-- {
		child = parents[i]
	}
	for _, lhs := range as.Lhs {
		if lhs == child {
			return true
		}
	}
	return false
}
