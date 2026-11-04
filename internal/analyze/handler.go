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
		valid, form := a.checkInput(method, in, fieldTypePos(decl.Type.Params, 1))
		if !valid {
			ok = false
		}
		h.Form = form
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
// Validate method if it has one. form reports whether the input holds files, whose body is a form.
func (a *analyzer) checkInput(method string, in types.Type, pos token.Pos) (ok, form bool) {
	label := method + " input"
	st, isStruct := in.Underlying().(*types.Struct)
	if !isStruct || isNamed(in, multipartPath, "FileHeader") {
		a.errorAt(pos, "%s %s must be a struct: the input is decoded into the fields of a struct", label, typeString(in))
		return false, false
	}
	ok = true
	if problems := checkInputTS(in, true); len(problems) > 0 {
		a.reportTS(pos, label, problems)
		ok = false
	}
	// The files of an upload are fields of the input itself: a form is a flat list of names.
	files := map[*types.Var]bool{}
	for i := range st.NumFields() {
		f := st.Field(i)
		if f.Exported() && !f.Embedded() && jsonName(st.Tag(i)) != "-" && fileType(f.Type()) {
			files[f] = true
		}
	}
	form = len(files) > 0
	root := rootLabel(in)
	walkFields(in, root, map[types.Type]bool{}, func(path string, f *types.Var, tag string) {
		if _, has := reflect.StructTag(tag).Lookup("form"); has {
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s has a form tag: an input field is named by its json tag only, also in a form body, so remove the form tag", label, path), f))
			ok = false
		}
		if !files[f] && holdsFile(f.Type()) {
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s holds a file as %s: a file can only be a field of the input itself, of type *multipart.FileHeader or []*multipart.FileHeader", label, path, types.TypeString(f.Type(), shortQualifier)), f))
			ok = false
		}
	})
	switch {
	case method == "GET" || method == "DELETE":
		for f := range files {
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s.%s is a file, which comes in a request body, and a %s input comes from the query string: take files in a POST, PUT or PATCH handler", label, root, f.Name(), method), f))
			ok = false
		}
		if !a.checkFlatInput(label, in, st, files, pos, fmt.Sprintf("a %s input comes from the query string", method)) {
			ok = false
		}
	case form:
		if !a.checkFlatInput(label, in, st, files, pos, fmt.Sprintf("a %s input that holds a file comes as a form", method)) {
			ok = false
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
	return ok, form
}

// checkFlatInput checks an input whose fields come as a flat list of names and string values: the
// query string of GET and DELETE, or the form of an input that holds files. why says which. Its
// fields are strings, numbers, bools or slices of them, and the files.
func (a *analyzer) checkFlatInput(label string, in types.Type, st *types.Struct, files map[*types.Var]bool, pos token.Pos, why string) bool {
	for _, name := range []string{"MarshalText", "UnmarshalText"} {
		if _, _, found := lookupName(in, name); found {
			a.errorAt(pos, "%s %s has %s: %s, which holds an object of fields, not a string", label, types.TypeString(in, shortQualifier), name, why)
			return false
		}
	}
	ok := true
	root := rootLabel(in)
	for i := range st.NumFields() {
		f := st.Field(i)
		path := root + "." + f.Name()
		switch {
		case f.Embedded():
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s is an embedded field: %s, so list its fields directly", label, path, why), f))
			ok = false
		case !f.Exported() || jsonName(st.Tag(i)) == "-" || files[f] || holdsFile(f.Type()):
		case !queryType(f.Type()):
			a.errorAt(pos, "%s", a.withField(fmt.Sprintf("%s: %s is %s: %s, so its fields are strings, numbers, bools or slices of them", label, path, types.TypeString(f.Type(), shortQualifier), why), f))
			ok = false
		}
	}
	return ok
}

// jsonName is the name of the json tag of a struct field, "" without one.
func jsonName(tag string) string {
	name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
	return name
}

// fileType reports whether t is *multipart.FileHeader or []*multipart.FileHeader, the types of the
// files of an upload in a field of an input.
func fileType(t types.Type) bool {
	if s, ok := types.Unalias(t).(*types.Slice); ok {
		t = s.Elem()
	}
	p, ok := types.Unalias(t).(*types.Pointer)
	return ok && isNamed(p.Elem(), multipartPath, "FileHeader")
}

// holdsFile reports whether t is a multipart.FileHeader or holds one through pointers, slices,
// arrays and maps. The fields of a struct are walked on their own.
func holdsFile(t types.Type) bool {
	for {
		if isNamed(t, multipartPath, "FileHeader") {
			return true
		}
		switch u := types.Unalias(t).(type) {
		case *types.Pointer:
			t = u.Elem()
		case *types.Slice:
			t = u.Elem()
		case *types.Array:
			t = u.Elem()
		case *types.Map:
			t = u.Elem()
		default:
			return false
		}
	}
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

// decodeProblem tells why json v2 would not read a value of type t from the JSON its
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
