package analyze

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

const handlerSignatures = "func(c *tifa.Ctx) error, func(c *tifa.Ctx, in In) error, func(c *tifa.Ctx) (Out, error) or func(c *tifa.Ctx, in In) (Out, error)"

// readHandler reads the method handler obj of route.go. It reports false, after reporting the
// violations, when the handler cannot be registered.
func (a *analyzer) readHandler(pkg *packages.Package, obj types.Object, method string) (Handler, bool) {
	fn, ok := obj.(*types.Func)
	var sig *types.Signature
	if ok {
		sig = fn.Signature()
		params, results := sig.Params(), sig.Results()
		ok = sig.TypeParams().Len() == 0 && !sig.Variadic() &&
			(params.Len() == 1 || params.Len() == 2) && typeString(params.At(0).Type()) == ctxType &&
			(results.Len() == 1 || results.Len() == 2) && typeString(results.At(results.Len()-1).Type()) == "error"
	}
	if !ok {
		a.errorAt(obj.Pos(), "%s must have one of the signatures %s", method, handlerSignatures)
		return Handler{}, false
	}
	decl := a.funcDecl(pkg, fn)
	h := Handler{Method: method}
	if sig.Params().Len() == 2 {
		in := sig.Params().At(1).Type()
		if !a.checkInput(method, in, fieldTypePos(decl.Type.Params, 1)) {
			ok = false
		}
		h.Input, h.InputType = typeString(in), in
	}
	if sig.Results().Len() == 2 {
		out := sig.Results().At(0).Type()
		pos := fieldTypePos(decl.Type.Results, 0)
		if problems := checkTS(out); len(problems) > 0 {
			a.reportTS(pos, method+" result", problems)
			ok = false
		}
		h.Output, h.OutputType = typeString(out), out
	}
	return h, ok
}

// fieldTypePos is the position of the type of the i-th parameter or result in list, where
// several names may share one field.
func fieldTypePos(list *ast.FieldList, i int) token.Pos {
	for _, f := range list.List {
		n := max(len(f.Names), 1)
		if i < n {
			return f.Type.Pos()
		}
		i -= n
	}
	panic("analyze: no field " + fmt.Sprint(i)) // the signature was checked against the list
}

// reportTS reports the problems checkTS found in a type whose source is at pos.
func (a *analyzer) reportTS(pos token.Pos, label string, problems []tsProblem) {
	for _, p := range problems {
		a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s: %s", label, p.path, p.msg), p.field))
	}
}

// withField appends where field is declared to msg.
func (a *analyzer) withField(msg string, field *types.Var) string {
	if field != nil && field.Pos().IsValid() {
		fp := a.fset.Position(field.Pos())
		msg += fmt.Sprintf(" (field declared at %s:%d)", a.relFile(fp.Filename), fp.Line)
	}
	return msg
}

// checkInput checks the input type of a method handler. The generated code decodes it from the
// request body as JSON or a form, or for GET and DELETE from the query string, then calls its
// Validate method if it has one.
func (a *analyzer) checkInput(method string, in types.Type, pos token.Pos) bool {
	label := method + " input"
	st, ok := in.Underlying().(*types.Struct)
	if !ok {
		a.errorAt(pos, "%s %s must be a struct: the input is decoded into the fields of a struct", label, typeString(in))
		return false
	}
	ok = true
	if problems := checkInputTS(in); len(problems) > 0 {
		a.reportTS(pos, label, problems)
		ok = false
	}
	root := rootLabel(in)
	walkFields(in, root, map[types.Type]bool{}, func(path string, f *types.Var, tag string) {
		if _, has := reflect.StructTag(tag).Lookup("form"); has {
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s has a form tag: an input field is named by its json tag only, also in a form body, so remove the form tag", label, path), f))
			ok = false
		}
	})
	if method == "GET" || method == "DELETE" {
		for _, name := range []string{"MarshalText", "UnmarshalText"} {
			if _, _, found := lookupName(in, name); found {
				a.errorAt(pos, "%s %s has %s: a %s input comes from the query string, which holds an object of fields, not a string", label, types.TypeString(in, shortQualifier), name, method)
				return false
			}
		}
		for i := range st.NumFields() {
			f := st.Field(i)
			path := root + "." + f.Name()
			name, _, _ := strings.Cut(reflect.StructTag(st.Tag(i)).Get("json"), ",")
			switch {
			case f.Embedded():
				a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s is an embedded field: a %s input comes from the query string, so list its fields directly", label, path, method), f))
				ok = false
			case !f.Exported() || name == "-":
			case !queryType(f.Type()):
				a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s is %s: a %s input comes from the query string, so its fields are strings, numbers, bools or slices of them", label, path, types.TypeString(f.Type(), shortQualifier), method), f))
				ok = false
			}
		}
	}
	if fn, _, _ := types.LookupFieldOrMethod(in, true, nil, "Validate"); fn != nil {
		if fn, isFunc := fn.(*types.Func); isFunc {
			s := fn.Signature()
			if s.Params().Len() != 0 || s.Results().Len() != 1 || typeString(s.Results().At(0).Type()) != "error" {
				a.errorAt(fn.Pos(), "Validate must have the signature func() error: it checks the input of %s before the handler runs", method)
				ok = false
			}
		}
	}
	return ok
}

// walkFields calls visit for every field of the structs that t holds, through pointers, slices,
// arrays and maps, with its path from the root and its tag. Each type is walked once, so a type
// that holds itself, such as type Tree map[string]Tree, ends the walk.
func walkFields(t types.Type, path string, seen map[types.Type]bool, visit func(path string, f *types.Var, tag string)) {
	if seen[t] {
		return
	}
	seen[t] = true
	switch u := types.Unalias(t).Underlying().(type) {
	case *types.Pointer:
		walkFields(u.Elem(), path, seen, visit)
	case *types.Slice:
		walkFields(u.Elem(), path+"[]", seen, visit)
	case *types.Array:
		walkFields(u.Elem(), path+"[]", seen, visit)
	case *types.Map:
		walkFields(u.Elem(), path+"[]", seen, visit)
	case *types.Struct:
		for i := range u.NumFields() {
			f := u.Field(i)
			p := path + "." + f.Name()
			visit(p, f, u.Tag(i))
			walkFields(f.Type(), p, seen, visit)
		}
	}
}

// queryType reports whether a query string value decodes into a field of type t the way its
// TypeScript type describes it: a bool, number or string kind without text methods, or a slice of
// one. A byte slice is base64 in JSON, so it is not one.
func queryType(t types.Type) bool {
	if s, ok := t.Underlying().(*types.Slice); ok {
		if b, ok := types.Unalias(s.Elem()).(*types.Basic); ok && b.Kind() == types.Byte {
			return false
		}
		t = s.Elem()
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok || b.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsString) == 0 {
		return false
	}
	for _, name := range []string{"MarshalText", "UnmarshalText"} {
		if _, _, found := lookupName(t, name); found {
			return false
		}
	}
	return true
}

var textUnmarshalSig = types.NewSignatureType(nil, nil, nil,
	types.NewTuple(types.NewVar(token.NoPos, nil, "", types.NewSlice(types.Typ[types.Byte]))),
	types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Universe.Lookup("error").Type())), false)

// decodeProblem tells why encoding/json would not read a value of type t from the JSON its
// TypeScript type describes, or returns "". checkTS has accepted t, so a MarshalText it finds
// is the one that makes t a string. A map key is a JSON string whatever its kind, so an
// UnmarshalText reads it when key is true.
func decodeProblem(t types.Type, key bool) string {
	name := types.TypeString(t, shortQualifier)
	for _, method := range []string{"UnmarshalJSON", "UnmarshalJSONFrom"} {
		if _, _, found := lookupName(t, method); found {
			return name + " has " + method + ", which reads JSON its own way, so it may not accept what its TypeScript type describes: rename the method or use another type"
		}
	}
	_, _, marshals := lookupName(t, "MarshalText")
	obj, _, unmarshals := lookupName(t, "UnmarshalText")
	switch {
	case marshals && !unmarshals:
		return name + " is a string in TypeScript because of MarshalText, but has no UnmarshalText to read the string back: add UnmarshalText([]byte) error"
	case !unmarshals:
		return ""
	}
	if fn, ok := obj.(*types.Func); !ok || !types.Identical(fn.Signature(), textUnmarshalSig) {
		return name + " has an UnmarshalText that is not the method UnmarshalText([]byte) error: give it that signature or rename it"
	}
	if b, ok := t.Underlying().(*types.Basic); !key && !marshals && (!ok || b.Info()&types.IsString == 0) {
		return name + " has UnmarshalText, so it is read from a JSON string, but its TypeScript type is not string: add MarshalText or remove UnmarshalText"
	}
	return ""
}
