package analyze

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// The TypeScript form of a Load result is the JSON that tifa writes for it: encoding/json/v2 with
// its defaults, map keys sorted and bytes that are no UTF-8 written as U+FFFD (core.JSONWrite).
// tsType accepts only types json v2 writes without an error and in a form the analyzer knows, and
// a test compares the result with the real output. Anything not listed here is rejected, and code
// generation relies on exactly these rules:
//
//   - bool is boolean. Integer kinds (uintptr included) and float kinds are number. string is string.
//   - time.Time is string. json.Number (of encoding/json) is number. time.Duration is rejected:
//     json v2 has no form for it.
//   - A pointer is T | null.
//   - A slice is T[], because a nil slice is []. A slice or array whose elements are the
//     predeclared byte or uint8 is string (base64, "" for a nil slice). The elements of a named
//     byte type make an ordinary array.
//   - An array is T[]. An array of length 0 is [] whatever its element type, because
//     json v2 never encodes an element.
//   - A map is Record<string, V>, because a nil map is {}. Its key is a string or integer kind, or
//     a type that is string by the MarshalText rule below.
//   - A struct is an object of its exported fields. Embedded exported structs (possibly through
//     a pointer) without a json name have their fields promoted. An embedded field of another
//     type needs a json name, an embedded struct may have no marshal or unmarshal method, and an
//     unexported embedded field is rejected unless its tag is json:"-". An unexported field may
//     have no json tag other than json:"-", and a struct that has fields needs an exported one or
//     a json tag. A field's name is the json tag name, or the Go field name when the tag name is
//     empty. The tag name must use only letters, digits and the punctuation
//     !#$%&()*+-./:;<=>?@[]^_{|}~ and space. json:"-" drops a field. Two fields of one struct may
//     not share a name. When fields of different structs share a name, the shallowest wins, then
//     the tagged one, and if that leaves a tie all of them are dropped, silently, as json v2 does.
//     The only tag options are omitempty and omitzero, which make a property optional, as does
//     reaching it through an embedded pointer, and string, which turns a field of a predeclared
//     number type, or an unnamed pointer to one, into string.
//   - A type with a method or field named MarshalJSON, MarshalJSONTo, AppendText or AppendJSON,
//     in the method set of its value or its pointer and whatever its signature, is rejected,
//     except time.Time and json.Number above.
//   - A type whose own MarshalText is exactly MarshalText() ([]byte, error), on the value or the
//     pointer receiver, is string: json v2 calls it on any value. A MarshalText of another
//     signature, promoted from an embedded field, or hidden by a field of that name is rejected.
//   - Named types keep their names, so recursive types are allowed.
//
// Rejected besides: every interface type, wherever it appears, chan, func, complex numbers,
// unsafe.Pointer and type parameters.
func checkTS(t types.Type) []tsProblem {
	_, problems := tsType(t)
	return problems
}

// checkInputTS is checkTS for the input of a method handler, which is decoded from that JSON.
func checkInputTS(t types.Type) []tsProblem {
	c := tsChecker{active: map[tsKey]bool{}, decode: true}
	c.encode(t, rootLabel(t), false)
	return c.problems
}

// tsType returns the TypeScript type of the JSON for a Load result of type t, written inline.
// A named type met again while it is being written is referred to by its Go name.
func tsType(t types.Type) (string, []tsProblem) {
	c := tsChecker{active: map[tsKey]bool{}}
	ts := c.encode(t, rootLabel(t), false)
	return ts, c.problems
}

// tsProblem is one part of a type with no TypeScript form. path locates it from the root type,
// such as "Order.Items[].Meta". field is the struct field that holds it, if any.
type tsProblem struct {
	path  string
	msg   string
	field *types.Var
}

type tsKey struct {
	typ    string
	quoted bool
}

type tsChecker struct {
	active   map[tsKey]bool // named types being written, to stop at recursion
	problems []tsProblem
	field    *types.Var // field being written, for the position of a problem
	decls    *tsDecls   // nil unless recursive types are declared, see TSShape
	// decode also requires each type written to read back from that JSON, for the input of a
	// method handler. Only what the JSON holds is checked: a field dropped or shadowed, the
	// inside of a type that is a string, and the element of an empty array are not.
	decode bool
}

func (c *tsChecker) fail(path, msg string) string {
	c.problems = append(c.problems, tsProblem{path: path, msg: msg, field: c.field})
	return "never"
}

// encoderMethods are the method names that json v2 calls
// instead of encoding a value by its kind, besides MarshalText.
var encoderMethods = []string{"MarshalJSON", "MarshalJSONTo", "AppendText", "AppendJSON"}

var textMarshalSig = types.NewSignatureType(nil, nil, nil, nil,
	types.NewTuple(types.NewVar(token.NoPos, nil, "", types.NewSlice(types.Typ[types.Byte])),
		types.NewVar(token.NoPos, nil, "", types.Universe.Lookup("error").Type())), false)

func rootLabel(t types.Type) string {
	for {
		switch u := types.Unalias(t).(type) {
		case *types.Pointer:
			t = u.Elem()
			continue
		case *types.Named:
			return u.Obj().Name()
		}
		return types.TypeString(t, shortQualifier)
	}
}

func shortQualifier(p *types.Package) string { return p.Name() }

func isNamed(t types.Type, pkg, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}

// lookupName finds a field or method called name on an addressable t, so the methods of *t
// count. It reports an ambiguous name as found with a nil object.
func lookupName(t types.Type, name string) (types.Object, []int, bool) {
	obj, index, _ := types.LookupFieldOrMethod(t, true, nil, name)
	return obj, index, obj != nil || index != nil
}

// textMethod checks the MarshalText of t. It returns whether t has one, and a problem when that
// one is not accepted. json v2 calls a MarshalText on the pointer receiver for any value too.
func textMethod(t types.Type, typeName string) (bool, string) {
	obj, index, found := lookupName(t, "MarshalText")
	if !found {
		return false, ""
	}
	fn, ok := obj.(*types.Func)
	if !ok || !types.Identical(fn.Signature(), textMarshalSig) {
		return true, typeName + " has a MarshalText that is not the method MarshalText() ([]byte, error), so json v2 may ignore it: give it that signature or rename it"
	}
	if len(index) > 1 {
		return true, typeName + " gets MarshalText from an embedded field: declare MarshalText on " + typeName + " itself"
	}
	return true, ""
}

// encode writes the TypeScript type of t. quoted is the string option of the enclosing field.
func (c *tsChecker) encode(t types.Type, path string, quoted bool) (ts string) {
	typeName := types.TypeString(t, shortQualifier) // before Unalias, so any stays any
	t = types.Unalias(t)
	if types.IsInterface(t) {
		// The dynamic type decides the JSON, and a nil interface is null.
		return c.fail(path, typeName+" has no fixed JSON form: it is an interface")
	}
	switch {
	case isNamed(t, "time", "Time"):
		return "string"
	case isNamed(t, "encoding/json", "Number"):
		return "number"
	case isNamed(t, "time", "Duration"):
		return c.fail(path, "time.Duration has no JSON form in json v2: use an integer of the unit you mean, such as milliseconds, or a string")
	}
	if _, isPtr := t.Underlying().(*types.Pointer); !isPtr {
		for _, name := range encoderMethods {
			if _, _, found := lookupName(t, name); found {
				return c.fail(path, typeName+" has a method or field named "+name+", which json v2 may call with output the analyzer cannot know: rename it, or return a type without it")
			}
		}
		if c.decode {
			if problem := decodeProblem(t, false); problem != "" {
				return c.fail(path, problem)
			}
		}
		if has, problem := textMethod(t, typeName); problem != "" {
			return c.fail(path, problem)
		} else if has {
			return "string"
		}
	}

	if n, ok := t.(*types.Named); ok {
		key := tsKey{types.TypeString(n, nil), quoted}
		if c.decls != nil {
			if name, ok := c.decls.declared[key]; ok {
				return name
			}
		}
		if c.active[key] {
			if c.decls != nil {
				return c.decls.name(key, n.Obj().Name())
			}
			return n.Obj().Name()
		}
		c.active[key] = true
		defer delete(c.active, key)
		if c.decls != nil {
			defer func() { ts = c.decls.declare(key, ts) }()
		}
	}

	switch u := t.Underlying().(type) {
	case *types.Basic:
		info := u.Info()
		switch {
		case info&types.IsBoolean != 0:
			return orString(quoted, "boolean")
		case info&(types.IsInteger|types.IsFloat) != 0:
			return orString(quoted, "number")
		case info&types.IsString != 0:
			return "string"
		}
		return c.fail(path, typeName+" has no JSON form")
	case *types.Pointer:
		return nullable(c.encode(u.Elem(), path, quoted))
	case *types.Slice:
		if isByte(u.Elem()) { // base64, and "" for a nil slice
			return "string"
		}
		return arrayOf(c.encode(u.Elem(), path+"[]", false))
	case *types.Array:
		if isByte(u.Elem()) { // base64, like a slice of bytes
			return "string"
		}
		if u.Len() == 0 { // always written as [], the element encoder never runs
			return "[]"
		}
		return arrayOf(c.encode(u.Elem(), path+"[]", false))
	case *types.Map:
		if problem := mapKeyProblem(u.Key()); problem != "" {
			return c.fail(path, typeName+" has no JSON form: "+problem)
		}
		if c.decode {
			if problem := decodeProblem(u.Key(), true); problem != "" {
				return c.fail(path+"[key]", problem)
			}
		}
		return "Record<string, " + c.encode(u.Elem(), path+"[key]", false) + ">"
	case *types.Struct:
		return c.object(u, path)
	}
	return c.fail(path, typeName+" has no JSON form") // chan, func, unsafe.Pointer
}

// mapKeyProblem says why a map key type is not accepted, or returns an empty string.
func mapKeyProblem(key types.Type) string {
	name := types.TypeString(key, shortQualifier)
	key = types.Unalias(key)
	if types.IsInterface(key) {
		return "the key " + name + " is an interface"
	}
	if _, ok := key.Underlying().(*types.Pointer); ok {
		return "the key " + name + " is a pointer"
	}
	for _, m := range encoderMethods {
		if _, _, found := lookupName(key, m); found {
			return "the key " + name + " has a method or field named " + m
		}
	}
	has, problem := textMethod(key, name)
	if problem != "" {
		return problem
	}
	if b, ok := key.Underlying().(*types.Basic); has || ok && b.Info()&(types.IsString|types.IsInteger) != 0 {
		return ""
	}
	return "map keys must be strings, integers or types with MarshalText() ([]byte, error)"
}

// isByte reports whether t is the predeclared byte or uint8, whose slices and arrays json v2
// writes as base64. A named byte type makes an ordinary array.
func isByte(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

func orString(quoted bool, ts string) string {
	if quoted {
		return "string"
	}
	return ts
}

func nullable(ts string) string {
	if strings.HasSuffix(ts, " | null") {
		return ts
	}
	return ts + " | null"
}

func arrayOf(ts string) string {
	if strings.Contains(ts, " | ") {
		return "(" + ts + ")[]"
	}
	return ts + "[]"
}

// jsonField is one property of a struct, as makeStructFields in json v2 finds it.
type jsonField struct {
	name     string
	tagged   bool
	index    []int
	v        *types.Var
	path     string // Go path from the root, such as "Order.Base.ID"
	optional bool
	quoted   bool
}

// object mirrors makeStructFields of json v2: a breadth-first walk over embedded structs, then the dominance rules.
func (c *tsChecker) object(s *types.Struct, path string) string {
	type level struct {
		s          *types.Struct
		key        string
		index      []int
		path       string
		viaPointer bool
	}
	current, next := []level{}, []level{{s: s, key: types.TypeString(s, nil), path: path}}
	var count, nextCount map[string]int
	visited := map[string]bool{}
	var fields []jsonField
	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, map[string]int{}
		for _, f := range current {
			if visited[f.key] {
				continue
			}
			visited[f.key] = true
			names := map[string]string{} // the Go name of each JSON name of this struct
			anyTag, anyField := false, false
			for i := 0; i < f.s.NumFields(); i++ {
				sf := f.s.Field(i)
				fieldPath := f.path + "." + sf.Name()
				problem := func(msg string) {
					c.problems = append(c.problems, tsProblem{path: fieldPath, field: sf, msg: msg})
				}
				tag, hasTag := reflect.StructTag(f.s.Tag(i)).Lookup("json")
				anyTag = anyTag || hasTag
				if tag == "-" {
					continue
				}
				if !sf.Exported() && !sf.Embedded() {
					if hasTag {
						problem("unexported field " + sf.Name() + " has a json tag, which json v2 refuses: export the field, remove the tag or tag it json:\"-\"")
					}
					continue
				}
				anyField = true
				if !sf.Exported() {
					problem("unexported embedded field " + sf.Name() + ": export its type, give the field a name or tag it json:\"-\"")
					continue
				}
				name, opts, hasOpts := strings.Cut(tag, ",")
				if name != "" && !validTagName(name) {
					problem(fmt.Sprintf("json tag name %q is not accepted: use letters, digits and the punctuation !#$%%&()*+-./:;<=>?@[]^_{|}~ or space", name))
					continue
				}
				var omit, quoted bool
				seen := map[string]bool{}
				bad := false
				if hasOpts {
					for _, o := range strings.Split(opts, ",") {
						switch {
						case o != "omitempty" && o != "omitzero" && o != "string":
							problem(fmt.Sprintf("json tag option %q is not accepted: use omitempty, omitzero or string", o))
							bad = true
						case seen[o]:
							problem(fmt.Sprintf("json tag option %q appears twice", o))
							bad = true
						}
						seen[o] = true
					}
				}
				if bad {
					continue
				}
				omit = seen["omitempty"] || seen["omitzero"]
				if seen["string"] {
					if !stringOptionType(sf.Type()) {
						problem("the string option is only accepted on fields of a predeclared number type, or an unnamed pointer to one: json v2 refuses it on other types, bool and string among them")
						continue
					}
					quoted = true
				}
				index := append(slices.Clone(f.index), i)

				ft := types.Unalias(sf.Type())
				ptr, unnamedPtr := ft.(*types.Pointer)
				if unnamedPtr {
					ft = types.Unalias(ptr.Elem())
				}
				_, ftStruct := ft.Underlying().(*types.Struct)
				if name == "" && sf.Embedded() {
					switch {
					case !ftStruct:
						problem("embedded field " + sf.Name() + " is not a struct, so json v2 needs a json name for it: tag it json:\"name\"")
						continue
					case hasOpts:
						problem("embedded field " + sf.Name() + " has json tag options, which json v2 refuses on an embedded struct: give it a json name or drop the options")
						continue
					}
					if m := embedMethod(ft); m != "" {
						problem("embedded field " + sf.Name() + " has a method or field named " + m + ", which json v2 refuses on an embedded struct: give the field a json name or rename " + m)
						continue
					}
				}
				if name != "" || !sf.Embedded() {
					tagged := name != ""
					if name == "" {
						name = sf.Name()
					}
					if other, ok := names[name]; ok {
						problem(fmt.Sprintf("fields %s and %s of one struct share the JSON name %q, which json v2 refuses: rename one or tag it json:\"-\"", other, sf.Name(), name))
						continue
					}
					names[name] = sf.Name()
					fields = append(fields, jsonField{
						name: name, tagged: tagged, index: index, v: sf, path: fieldPath,
						optional: omit || f.viaPointer,
						quoted:   quoted,
					})
					if count[f.key] > 1 { // the same struct embedded twice at this depth: its fields cancel out
						fields = append(fields, fields[len(fields)-1])
					}
					continue
				}
				key := types.TypeString(ft, nil)
				nextCount[key]++
				if nextCount[key] == 1 {
					next = append(next, level{
						s: ft.Underlying().(*types.Struct), key: key, index: index, path: fieldPath,
						viaPointer: f.viaPointer || unnamedPtr,
					})
				}
			}
			if f.s.NumFields() > 0 && !anyTag && !anyField {
				c.problems = append(c.problems, tsProblem{path: f.path, field: c.field,
					msg: types.TypeString(f.s, shortQualifier) + " has fields but none json v2 can write, which it refuses: export a field, or tag one json:\"-\""})
			}
		}
	}

	slices.SortStableFunc(fields, func(a, b jsonField) int {
		if c := strings.Compare(a.name, b.name); c != 0 {
			return c
		}
		if len(a.index) != len(b.index) {
			return len(a.index) - len(b.index)
		}
		if a.tagged != b.tagged {
			if a.tagged {
				return -1
			}
			return 1
		}
		return slices.Compare(a.index, b.index)
	})
	var kept []jsonField
	for i := 0; i < len(fields); {
		j := i + 1
		for j < len(fields) && fields[j].name == fields[i].name {
			j++
		}
		group := fields[i:j]
		if len(group) == 1 || len(group[0].index) != len(group[1].index) || group[0].tagged != group[1].tagged {
			kept = append(kept, group[0])
		}
		i = j
	}
	slices.SortFunc(kept, func(a, b jsonField) int { return slices.Compare(a.index, b.index) })

	outer := c.field
	defer func() { c.field = outer }()
	props := make([]string, len(kept))
	for i, f := range kept {
		c.field = f.v
		ts := c.encode(f.v.Type(), f.path, f.quoted)
		opt := ""
		if f.optional {
			opt = "?"
		}
		props[i] = tsPropertyName(f.name) + opt + ": " + ts
	}
	if len(props) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(props, "; ") + " }"
}

// stringOptionType reports whether the string tag option is accepted on a field of type t: json v2
// takes it only on numbers.
func stringOptionType(t types.Type) bool {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	b, ok := t.(*types.Basic)
	return ok && b.Info()&(types.IsInteger|types.IsFloat) != 0
}

// embedMethods are the methods json v2 refuses on an embedded struct, since it could not tell which
// fields to promote.
var embedMethods = []string{"MarshalJSON", "MarshalJSONTo", "MarshalText", "AppendText", "UnmarshalJSON", "UnmarshalJSONFrom", "UnmarshalText"}

// embedMethod returns the first name of embedMethods that the struct t, or its pointer, has as a
// method or field, or an empty string.
func embedMethod(t types.Type) string {
	for _, m := range embedMethods {
		if _, _, found := lookupName(t, m); found {
			return m
		}
	}
	return ""
}

// validTagName is isValidTag of encoding/json: the names json v1 took, which json v2 reads alike.
func validTagName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

// tsPropertyName quotes a property name that is not a plain identifier.
func tsPropertyName(name string) string {
	for i, c := range name {
		if !(c == '_' || c == '$' || unicode.IsLetter(c) || i > 0 && unicode.IsDigit(c)) {
			return `"` + strings.ReplaceAll(name, `"`, `\"`) + `"`
		}
	}
	return name
}
