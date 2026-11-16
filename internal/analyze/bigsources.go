package analyze

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
)

// Tifa refuses at run time to send a number that a browser would read as another one (see
// core.CheckBrowserNumbers). The build warns early where the code itself shows such a number going
// into a field that a page reads, and only in these direct forms:
//
//   - a field of a composite literal (Row{ID: x.UnixNano()}), keyed or not, or an assignment to a
//     field (r.ID = h.Sum64())
//   - the value is a constant above 2^53, or a call whose result is large by its nature: the
//     UnixNano of a time.Time, the unbounded numbers of math/rand and math/rand/v2 (Int, Uint,
//     Int63, Int64, Uint64), a function or method named Sum64, or starting with Sum64, that returns
//     a uint64 (hashes: fnv, maphash, xxhash and the like), and the Uint64 of an encoding/binary
//     byte order read from a buffer that crypto/rand filled in the same function
//   - a conversion around the value to a 64-bit integer type, and one step through a local
//     variable assigned once in the same function
//
// A field is looked at when it is in a type that reaches a page (the results of Load, Layout and
// the handlers, and the Exposed data of the session), has an integer type and has no
// json:",string". Nothing is followed from one function to another, and nothing is guessed from
// the name or the type of a field alone.

// Warning is a problem that does not stop a build, at a source position.
type Warning struct {
	Pos token.Position // Filename is relative to the application root
	Msg string
}

func (w Warning) String() string { return Error(w).Error() }

const maxSafeInteger = 1<<53 - 1

func (a *analyzer) bigSourceWarnings(m *Manifest) []Warning {
	fields := map[*types.Var]bool{}
	seen := map[types.Type]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		switch t := types.Unalias(t).(type) {
		case *types.Named:
			if seen[t] {
				return
			}
			seen[t] = true
			walk(t.Underlying())
		case *types.Pointer:
			walk(t.Elem())
		case *types.Slice:
			walk(t.Elem())
		case *types.Array:
			walk(t.Elem())
		case *types.Map:
			walk(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				f := t.Field(i)
				tag, _ := reflect.StructTag(t.Tag(i)).Lookup("json")
				if tag == "-" || !f.Exported() && !f.Embedded() {
					continue
				}
				_, opts, _ := strings.Cut(tag, ",")
				quoted := slices.Contains(strings.Split(opts, ","), "string")
				switch {
				case hasMarshalText(f.Type()):
				case isWideInteger(f.Type()):
					if !quoted {
						fields[f.Origin()] = true
					}
				default:
					walk(f.Type())
				}
			}
		}
	}
	for _, r := range m.Routes {
		if r.Load != nil && r.Load.Result != nil {
			walk(r.Load.Result)
		}
		for _, h := range r.Handlers {
			if h.OutputType != nil {
				walk(h.OutputType)
			}
		}
	}
	for _, l := range m.Layouts {
		if l.Load != nil && l.Load.Result != nil {
			walk(l.Load.Result)
		}
	}
	if m.Session != nil && m.Session.ExposedResult != nil {
		walk(m.Session.ExposedResult)
	}
	if len(fields) == 0 {
		return nil
	}

	var out []Warning
	reported := map[token.Pos]bool{}
	for _, pkg := range a.modulePackages {
		if pkg.TypesInfo == nil {
			continue
		}
		report := func(value ast.Expr, owner types.Type, f *types.Var, reason string) {
			if reported[value.Pos()] {
				return
			}
			reported[value.Pos()] = true
			p := a.fset.Position(value.Pos())
			p.Filename = a.relFile(p.Filename)
			out = append(out, Warning{Pos: p, Msg: fmt.Sprintf("%s.%s gets %s, a number above 2^53. A browser would read another number, so the request will be refused at run time. "+
				"Tag the field `json:\",string\"` (TypeScript string), or keep the value smaller (for a time, use UnixMilli)", ownerName(owner), f.Name(), reason)})
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				w := &sourceWalker{info: pkg.TypesInfo, fields: fields, report: report}
				if fn, ok := decl.(*ast.FuncDecl); ok {
					if fn.Body == nil {
						continue
					}
					w.scanLocals(fn.Body)
				}
				ast.Inspect(decl, w.visit)
			}
		}
	}
	slices.SortStableFunc(out, func(x, y Warning) int {
		return cmp.Or(strings.Compare(x.Pos.Filename, y.Pos.Filename), cmp.Compare(x.Pos.Line, y.Pos.Line), cmp.Compare(x.Pos.Column, y.Pos.Column))
	})
	return out
}

// ownerName is the name of the struct type that holds a field, for a message.
func ownerName(t types.Type) string {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Name()
	}
	return "struct"
}

// isWideInteger reports whether t, through pointers, is an integer type that can hold a number
// above 2^53.
func isWideInteger(t types.Type) bool {
	for {
		p, ok := types.Unalias(t).Underlying().(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	b, ok := types.Unalias(t).Underlying().(*types.Basic)
	if !ok {
		return false
	}
	switch b.Kind() {
	case types.Int, types.Int64, types.Uint, types.Uint64, types.Uintptr:
		return true
	}
	return false
}

// hasMarshalText reports whether t is written as a string by its own MarshalText.
func hasMarshalText(t types.Type) bool {
	if _, ok := types.Unalias(t).(*types.Named); !ok {
		return false
	}
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(t), true, nil, "MarshalText")
	_, ok := obj.(*types.Func)
	return ok
}

// sourceWalker looks at the composite literals and the assignments of one declaration.
type sourceWalker struct {
	info   *types.Info
	fields map[*types.Var]bool
	report func(value ast.Expr, owner types.Type, f *types.Var, reason string)
	// assigns holds the values a local variable of the function is given, nil for a form that
	// is not a plain assignment. randBufs holds the variables crypto/rand filled.
	assigns  map[*types.Var][]ast.Expr
	randBufs map[*types.Var]bool
}

func (w *sourceWalker) scanLocals(body *ast.BlockStmt) {
	w.assigns, w.randBufs = map[*types.Var][]ast.Expr{}, map[*types.Var]bool{}
	local := func(id *ast.Ident) *types.Var {
		v, _ := w.info.ObjectOf(id).(*types.Var)
		if v == nil || v.IsField() || v.Parent() == nil || v.Parent() == v.Pkg().Scope() {
			return nil
		}
		return v
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			plain := (n.Tok == token.ASSIGN || n.Tok == token.DEFINE) && len(n.Lhs) == len(n.Rhs)
			for i, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok && local(id) != nil {
					var value ast.Expr
					if plain {
						value = n.Rhs[i]
					}
					w.assigns[local(id)] = append(w.assigns[local(id)], value)
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				if local(id) != nil {
					var value ast.Expr
					if len(n.Values) == len(n.Names) {
						value = n.Values[i]
					}
					w.assigns[local(id)] = append(w.assigns[local(id)], value)
				}
			}
		case *ast.CallExpr:
			if fn := w.callee(n.Fun); fn != nil && fn.Pkg() != nil {
				switch {
				case fn.Pkg().Path() == "crypto/rand" && fn.Name() == "Read" && len(n.Args) == 1:
					w.markBuffer(n.Args[0])
				case fn.Pkg().Path() == "io" && fn.Name() == "ReadFull" && len(n.Args) == 2 && w.isCryptoReader(n.Args[0]):
					w.markBuffer(n.Args[1])
				}
			}
		}
		return true
	})
}

func (w *sourceWalker) isCryptoReader(e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	v, _ := w.info.Uses[sel.Sel].(*types.Var)
	return v != nil && v.Pkg() != nil && v.Pkg().Path() == "crypto/rand" && v.Name() == "Reader"
}

func (w *sourceWalker) markBuffer(e ast.Expr) {
	if v := w.rootVar(e); v != nil {
		w.randBufs[v] = true
	}
}

// rootVar is the variable under b, b[:], b[:8], &b and (b).
func (w *sourceWalker) rootVar(e ast.Expr) *types.Var {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.SliceExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.UnaryExpr:
			e = x.X
		case *ast.Ident:
			v, _ := w.info.ObjectOf(x).(*types.Var)
			return v
		default:
			return nil
		}
	}
}

func (w *sourceWalker) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CompositeLit:
		st, _ := types.Unalias(w.info.TypeOf(n)).Underlying().(*types.Struct)
		if st == nil {
			return true
		}
		for i, e := range n.Elts {
			var f *types.Var
			value := e
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				key, _ := kv.Key.(*ast.Ident)
				if key == nil {
					continue
				}
				f, _ = w.info.Uses[key].(*types.Var)
				value = kv.Value
			} else if i < st.NumFields() {
				f = st.Field(i)
			}
			w.check(value, w.info.TypeOf(n), f)
		}
	case *ast.AssignStmt:
		if n.Tok != token.ASSIGN || len(n.Lhs) != len(n.Rhs) {
			return true
		}
		for i, l := range n.Lhs {
			sel, ok := l.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if s := w.info.Selections[sel]; s != nil && s.Kind() == types.FieldVal {
				f, _ := s.Obj().(*types.Var)
				w.check(n.Rhs[i], s.Recv(), f)
			}
		}
	}
	return true
}

func (w *sourceWalker) check(value ast.Expr, owner types.Type, f *types.Var) {
	if f == nil || !w.fields[f.Origin()] {
		return
	}
	if reason := w.big(value, 0); reason != "" {
		w.report(value, owner, f, reason)
	}
}

// big says what makes the value of e a number above 2^53, "" when the form is not one that shows it.
func (w *sourceWalker) big(e ast.Expr, steps int) string {
	e = ast.Unparen(e)
	if tv, ok := w.info.Types[e]; ok && tv.Value != nil {
		if v := constant.ToInt(tv.Value); v.Kind() == constant.Int &&
			(constant.Compare(v, token.GTR, constant.MakeInt64(maxSafeInteger)) || constant.Compare(v, token.LSS, constant.MakeInt64(-maxSafeInteger))) {
			return "the constant " + v.ExactString()
		}
		return ""
	}
	switch e := e.(type) {
	case *ast.CallExpr:
		if tv := w.info.Types[e.Fun]; tv.IsType() {
			if len(e.Args) == 1 && isWideInteger(tv.Type) {
				return w.big(e.Args[0], steps)
			}
			return ""
		}
		fn := w.callee(e.Fun)
		if fn == nil || fn.Pkg() == nil {
			return ""
		}
		path, name := fn.Pkg().Path(), fn.Name()
		sig := fn.Signature()
		switch {
		case path == "time" && name == "UnixNano" && sig.Recv() != nil:
			return "a value from time.Time.UnixNano"
		case (path == "math/rand" || path == "math/rand/v2") && slices.Contains([]string{"Int", "Uint", "Int63", "Int64", "Uint64"}, name):
			return "a value from " + path + "." + name
		case strings.HasPrefix(name, "Sum64") && sig.Results().Len() == 1 && isUint64(sig.Results().At(0).Type()):
			return "a value from " + name + ", a 64-bit hash"
		case path == "encoding/binary" && name == "Uint64" && sig.Recv() != nil && len(e.Args) == 1:
			if v := w.rootVar(e.Args[0]); v != nil && w.randBufs[v] {
				return "a value from " + types.ExprString(e.Fun) + " of bytes from crypto/rand"
			}
		}
	case *ast.Ident:
		if v, _ := w.info.Uses[e].(*types.Var); v != nil && steps == 0 {
			if values := w.assigns[v]; len(values) == 1 && values[0] != nil {
				return w.big(values[0], 1)
			}
		}
	}
	return ""
}

func isUint64(t types.Type) bool {
	b, ok := types.Unalias(t).Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint64
}

// callee is the function or method that a call of fun runs, nil for anything else.
func (w *sourceWalker) callee(fun ast.Expr) *types.Func {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		fn, _ := w.info.Uses[f].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		if s := w.info.Selections[f]; s != nil {
			fn, _ := s.Obj().(*types.Func)
			return fn
		}
		fn, _ := w.info.Uses[f.Sel].(*types.Func)
		return fn
	}
	return nil
}
