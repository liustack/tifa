package analyze

import (
	"go/ast"
	"go/token"
	"go/types"
)

// paramSig is the signature of Ctx.Param and Ctx.Query.
var paramSig = types.NewSignatureType(nil, nil, nil,
	types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.String])),
	types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.String])), false)

// checkCtxToInterface reports every place in body, closures included, where a value carrying the
// Ctx methods Param or Query is converted to an interface that declares Param or Query: explicit
// conversions, assignments, variable declarations, call arguments, returns, composite literal
// elements and channel sends. A call through such an interface hides the parameter name from
// the analyzer. Converting to any, or to an interface without these methods, is fine because
// neither method can be called through it.
func (a *analyzer) checkCtxToInterface(info *types.Info, fn *types.Signature, body *ast.BlockStmt) {
	check := func(target types.Type, e ast.Expr) {
		if target == nil || e == nil {
			return
		}
		iface, ok := target.Underlying().(*types.Interface)
		if !ok || !carriesCtxMethods(info.TypeOf(e)) {
			return
		}
		for _, name := range []string{"Param", "Query"} {
			if declaresParamMethod(iface, name) {
				a.errorAt(e.Pos(), "the Ctx is converted to %s, whose %s method the analyzer cannot follow: call c.%s directly",
					types.TypeString(target, shortQualifier), name, name)
				return
			}
		}
	}
	sigs := []*types.Signature{fn} // enclosing function literals, innermost last
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			sig, _ := info.TypeOf(n).(*types.Signature)
			sigs = append(sigs, sig)
			ast.Inspect(n.Body, visit)
			sigs = sigs[:len(sigs)-1]
			return false
		case *ast.CallExpr:
			if tv := info.Types[n.Fun]; tv.IsType() {
				if len(n.Args) == 1 {
					check(tv.Type, n.Args[0])
				}
				return true
			}
			if id := calleeIdent(n); id != nil {
				if b, ok := info.Uses[id].(*types.Builtin); ok && b.Name() == "append" && len(n.Args) > 1 && !n.Ellipsis.IsValid() {
					if s, ok := info.TypeOf(n.Args[0]).Underlying().(*types.Slice); ok {
						for _, arg := range n.Args[1:] {
							check(s.Elem(), arg)
						}
					}
					return true
				}
			}
			sig, ok := info.TypeOf(n.Fun).Underlying().(*types.Signature)
			if !ok {
				return true
			}
			for i, arg := range n.Args {
				params := sig.Params()
				switch {
				case sig.Variadic() && i >= params.Len()-1:
					last := params.At(params.Len() - 1).Type()
					if n.Ellipsis.IsValid() {
						check(last, arg)
					} else {
						check(last.(*types.Slice).Elem(), arg)
					}
				case i < params.Len():
					check(params.At(i).Type(), arg)
				}
			}
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i := range n.Lhs {
					check(info.TypeOf(n.Lhs[i]), n.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			if n.Type != nil && len(n.Names) == len(n.Values) {
				for _, v := range n.Values {
					check(info.TypeOf(n.Type), v)
				}
			}
		case *ast.ReturnStmt:
			if sig := sigs[len(sigs)-1]; sig != nil && sig.Results().Len() == len(n.Results) {
				for i, r := range n.Results {
					check(sig.Results().At(i).Type(), r)
				}
			}
		case *ast.SendStmt:
			if ch, ok := info.TypeOf(n.Chan).Underlying().(*types.Chan); ok {
				check(ch.Elem(), n.Value)
			}
		case *ast.CompositeLit:
			a.checkCompositeLit(info, n, check)
		}
		return true
	}
	ast.Inspect(body, visit)
}

func (a *analyzer) checkCompositeLit(info *types.Info, lit *ast.CompositeLit, check func(types.Type, ast.Expr)) {
	t := info.TypeOf(lit)
	if t == nil {
		return
	}
	if p, ok := t.Underlying().(*types.Pointer); ok { // &T{...} elided in a composite literal
		t = p.Elem()
	}
	for i, elt := range lit.Elts {
		key, value := ast.Expr(nil), elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			key, value = kv.Key, kv.Value
		}
		switch u := t.Underlying().(type) {
		case *types.Struct:
			if id, ok := key.(*ast.Ident); ok {
				for j := 0; j < u.NumFields(); j++ {
					if u.Field(j).Name() == id.Name {
						check(u.Field(j).Type(), value)
					}
				}
			} else if key == nil && i < u.NumFields() {
				check(u.Field(i).Type(), value)
			}
		case *types.Slice:
			check(u.Elem(), value)
		case *types.Array:
			check(u.Elem(), value)
		case *types.Map:
			check(u.Key(), key)
			check(u.Elem(), value)
		}
	}
}

// carriesCtxMethods reports whether a non-interface type t has the Ctx's own Param or Query
// method in its method set, as *tifa.Ctx and structs embedding it do.
func carriesCtxMethods(t types.Type) bool {
	if t == nil || types.IsInterface(t) {
		return false
	}
	for _, name := range []string{"Param", "Query"} {
		if obj, _, _ := types.LookupFieldOrMethod(t, false, nil, name); paramMethod(obj) != "" {
			return true
		}
	}
	return false
}

// declaresParamMethod reports whether iface has a method name with the signature func(string) string.
func declaresParamMethod(iface *types.Interface, name string) bool {
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		if m.Name() == name && types.Identical(m.Signature(), paramSig) {
			return true
		}
	}
	return false
}
