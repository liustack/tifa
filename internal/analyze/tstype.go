package analyze

import (
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// The TypeScript form of a Load result is the JSON that encoding/json (Go 1.26, json v1) writes
// for it. The server encodes the result value itself, as json.Marshal(result) does, so the root
// value is not addressable. tsType follows newTypeEncoder and typeFields of encoding/json step
// by step, and code generation relies on exactly these rules:
//
//   - A type that implements json.Marshaler is written by its MarshalJSON. Its form is only known
//     for time.Time (string), also when time.Time's method is promoted through embedding. Any
//     other MarshalJSON is rejected, because its output cannot be seen statically.
//   - A type that implements encoding.TextMarshaler is string.
//   - Method sets follow encoding/json: a non-pointer value whose pointer type implements the
//     interface uses it only when the value is addressable. Values behind a pointer, slice
//     elements and fields reached through an embedded pointer are addressable. The root value,
//     map values and fields of a non-addressable struct are not.
//   - bool is boolean. Integer kinds (uintptr included) and float kinds are number. string is
//     string, except json.Number, which is number.
//   - A pointer is T | null.
//   - A slice is T[] | null, because a nil slice is null. A slice whose element kind is uint8 is
//     string | null (base64), unless a pointer to the element implements json.Marshaler or
//     encoding.TextMarshaler.
//   - An array is T[], an array of bytes included. An array of length 0 is [] whatever its
//     element type, because encoding/json never encodes an element.
//   - A map is Record<string, V> | null, because a nil map is null. Keys of string kind, of
//     integer kinds, or implementing encoding.TextMarshaler become strings. Other keys are rejected.
//   - A struct is an object. Exported fields are kept, and embedded structs (exported or not,
//     possibly through a pointer) without a json name have their fields promoted. A field's name
//     is the json tag name, or the Go field name when the tag name is empty or not valid for
//     encoding/json. json:"-" drops a field. When several fields share a name, the shallowest
//     wins, then the tagged one, and if that leaves a tie all of them are dropped, silently, as
//     encoding/json does. omitempty or omitzero make a property optional, and so does reaching it
//     through an embedded pointer, which skips the field when nil. The ",string" option turns
//     bool, number and string fields (also behind an unnamed pointer) into string.
//   - Named types keep their names, so recursive types are allowed.
//
// Rejected: every interface type (any, error, interfaces with MarshalText and others), because
// the dynamic value decides the JSON and may be nil. Also rejected: chan, func, complex numbers,
// unsafe.Pointer, maps with other keys, and MarshalJSON other than time.Time's.
func checkTS(t types.Type) []tsProblem {
	_, problems := tsType(t)
	return problems
}

// tsType returns the TypeScript type of the JSON for a Load result of type t, written inline.
// A named type met again while it is being written is referred to by its Go name.
func tsType(t types.Type) (string, []tsProblem) {
	c := tsChecker{active: map[tsKey]bool{}}
	ts := c.encode(t, rootLabel(t), false, false)
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
	typ         string
	addressable bool
	quoted      bool
}

type tsChecker struct {
	active   map[tsKey]bool // named types being written, to stop at recursion
	problems []tsProblem
	field    *types.Var // field being written, for the position of a problem
}

func (c *tsChecker) fail(path, msg string) string {
	c.problems = append(c.problems, tsProblem{path: path, msg: msg, field: c.field})
	return "never"
}

var (
	errorType     = types.Universe.Lookup("error").Type()
	byteSlice     = types.NewSlice(types.Typ[types.Byte])
	marshalerType = methodInterface("MarshalJSON")
	textMarshaler = methodInterface("MarshalText")
)

// methodInterface builds interface{ name() ([]byte, error) }, the shape of json.Marshaler and
// encoding.TextMarshaler.
func methodInterface(name string) *types.Interface {
	results := types.NewTuple(types.NewVar(token.NoPos, nil, "", byteSlice), types.NewVar(token.NoPos, nil, "", errorType))
	sig := types.NewSignatureType(nil, nil, nil, nil, results, false)
	return types.NewInterfaceType([]*types.Func{types.NewFunc(token.NoPos, nil, name, sig)}, nil).Complete()
}

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

// encode mirrors newTypeEncoder. quoted is the ",string" option of the enclosing field.
func (c *tsChecker) encode(t types.Type, path string, addressable, quoted bool) string {
	typeName := types.TypeString(t, shortQualifier) // before Unalias, so any stays any
	t = types.Unalias(t)
	if types.IsInterface(t) {
		// The dynamic type decides the JSON, and a nil interface is null. Methods of the interface
		// itself, such as MarshalText, say nothing about what the value implements besides them.
		return c.fail(path, typeName+" has no fixed JSON form: it is an interface")
	}
	_, isPtr := t.Underlying().(*types.Pointer)

	if !isPtr && addressable && types.Implements(types.NewPointer(t), marshalerType) {
		return c.marshalJSON(types.NewPointer(t), path, typeName, false) // the address of a value is never nil
	}
	if types.Implements(t, marshalerType) {
		return c.marshalJSON(t, path, typeName, isPtr)
	}
	if !isPtr && addressable && types.Implements(types.NewPointer(t), textMarshaler) {
		return "string"
	}
	if types.Implements(t, textMarshaler) {
		if isPtr {
			return "string | null" // a nil pointer is written as null
		}
		return "string"
	}

	if n, ok := t.(*types.Named); ok {
		key := tsKey{types.TypeString(n, nil), addressable, quoted}
		if c.active[key] {
			return n.Obj().Name()
		}
		c.active[key] = true
		defer delete(c.active, key)
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
			if isNamed(t, "encoding/json", "Number") {
				return orString(quoted, "number")
			}
			return "string"
		}
		return c.fail(path, typeName+" has no JSON form")
	case *types.Pointer:
		return nullable(c.encode(u.Elem(), path, true, quoted))
	case *types.Slice:
		if b, ok := u.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 {
			p := types.NewPointer(u.Elem())
			if !types.Implements(p, marshalerType) && !types.Implements(p, textMarshaler) {
				return "string | null"
			}
		}
		return arrayOf(c.encode(u.Elem(), path+"[]", true, false)) + " | null"
	case *types.Array:
		if u.Len() == 0 { // always written as [], the element encoder never runs
			return "[]"
		}
		return arrayOf(c.encode(u.Elem(), path+"[]", addressable, false))
	case *types.Map:
		key := u.Key()
		b, ok := key.Underlying().(*types.Basic)
		if !(ok && b.Info()&(types.IsString|types.IsInteger) != 0) && !types.Implements(key, textMarshaler) {
			return c.fail(path, typeName+" has no JSON form: map keys must be strings, integers or implement encoding.TextMarshaler")
		}
		return "Record<string, " + c.encode(u.Elem(), path+"[key]", false, false) + "> | null"
	case *types.Struct:
		return c.object(u, path, addressable)
	}
	return c.fail(path, typeName+" has no JSON form") // chan, func, type parameters
}

// marshalJSON handles a value written by the MarshalJSON method of t. Only time.Time's method
// has a known output. nilable is true when the value is a pointer, written as null when nil.
func (c *tsChecker) marshalJSON(t types.Type, path, typeName string, nilable bool) string {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "MarshalJSON")
	if fn, ok := obj.(*types.Func); ok && isNamed(derefType(fn.Signature().Recv().Type()), "time", "Time") {
		if nilable {
			return "string | null"
		}
		return "string"
	}
	return c.fail(path, typeName+" implements json.Marshaler, so its JSON form cannot be known statically")
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

// jsonField is one property of a struct, as typeFields in encoding/json finds it.
type jsonField struct {
	name        string
	tagged      bool
	index       []int
	v           *types.Var
	path        string // Go path from the root, such as "Order.Base.ID"
	optional    bool
	quoted      bool
	addressable bool
}

// object mirrors typeFields: a breadth-first walk over embedded structs, then the dominance rules.
func (c *tsChecker) object(s *types.Struct, path string, addressable bool) string {
	type level struct {
		s           *types.Struct
		key         string
		index       []int
		path        string
		viaPointer  bool
		addressable bool
	}
	current, next := []level{}, []level{{s: s, key: types.TypeString(s, nil), path: path, addressable: addressable}}
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
			for i := 0; i < f.s.NumFields(); i++ {
				sf := f.s.Field(i)
				if sf.Embedded() {
					t := types.Unalias(sf.Type())
					if p, ok := t.(*types.Pointer); ok {
						t = p.Elem()
					}
					if _, isStruct := t.Underlying().(*types.Struct); !sf.Exported() && !isStruct {
						continue
					}
				} else if !sf.Exported() {
					continue
				}
				tag := reflect.StructTag(f.s.Tag(i)).Get("json")
				if tag == "-" {
					continue
				}
				name, opts, _ := strings.Cut(tag, ",")
				if !validTagName(name) {
					name = ""
				}
				index := append(slices.Clone(f.index), i)

				ft := types.Unalias(sf.Type())
				ptr, unnamedPtr := ft.(*types.Pointer)
				if unnamedPtr {
					ft = types.Unalias(ptr.Elem())
				}
				quoted := false
				if hasOption(opts, "string") {
					if b, ok := ft.Underlying().(*types.Basic); ok && b.Info()&(types.IsBoolean|types.IsNumeric|types.IsString) != 0 &&
						b.Info()&types.IsComplex == 0 {
						quoted = true
					}
				}
				_, ftStruct := ft.Underlying().(*types.Struct)
				if name != "" || !sf.Embedded() || !ftStruct {
					tagged := name != ""
					if name == "" {
						name = sf.Name()
					}
					fields = append(fields, jsonField{
						name: name, tagged: tagged, index: index, v: sf, path: f.path + "." + sf.Name(),
						optional:    hasOption(opts, "omitempty") || hasOption(opts, "omitzero") || f.viaPointer,
						quoted:      quoted,
						addressable: f.addressable,
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
						s: ft.Underlying().(*types.Struct), key: key, index: index, path: f.path + "." + sf.Name(),
						viaPointer: f.viaPointer || unnamedPtr, addressable: f.addressable || unnamedPtr,
					})
				}
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
		ts := c.encode(f.v.Type(), f.path, f.addressable, f.quoted)
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

func hasOption(opts, name string) bool {
	return slices.Contains(strings.Split(opts, ","), name)
}

// validTagName is isValidTag of encoding/json.
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
