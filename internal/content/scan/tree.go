package scan

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
)

// Kind is the shape of a value of the front matter.
type Kind int

const (
	String Kind = iota + 1
	Bool
	Int
	Uint
	Float
	Time
	Pointer
	Slice
	Map
	Struct
)

// Node describes what the front matter may hold for one Go type of a schema. It is made from the
// type with go/types in the analysis (FromTypes) and with reflect in tests (FromReflect), and the
// two agree. A named struct type met again inside itself is the same *Node, so trees may hold
// cycles.
type Node struct {
	Kind Kind
	// Bits is the size of an Int, Uint or Float: 8, 16, 32 or 64.
	Bits int
	// Elem is the element of a Pointer, Slice or Map.
	Elem *Node
	// Fields are those of a Struct, embedded structs flattened, in the order of the source.
	Fields []*Field
}

// Field is one key of a Struct.
type Field struct {
	// Key is the json name, which is the key of the front matter.
	Key string
	// Go names the field for messages: the schema type and the Go field names that lead to it,
	// such as myblog.Post.Title.
	Go string
	// Required is true unless the field is a pointer, a slice or a map, or has omitempty or omitzero.
	Required bool
	Node     *Node
}

// Problem is a schema field that front matter cannot hold.
type Problem struct {
	// Pos is the position of the field in the analysis, token.NoPos from reflect.
	Pos token.Pos
	Msg string
}

// reserved are the json names of content.Entry, with what Entry uses each for.
var reserved = map[string]string{
	"id":           "for the ID of an entry, which its path gives",
	"lang":         "for the language of an entry",
	"slug":         "for the last segment of the URL of an entry",
	"path":         "for the ID of an entry with its slug",
	"date":         "for the time an entry is published",
	"draft":        "to mark a draft",
	"translations": "for the versions of an entry in other languages",
	"words":        "for the number of words of the body",
}

const (
	entryPkg  = "github.com/liustack/tifa/content"
	entryName = "Entry"
)

// goType is the part of a Go type the tree needs, read with reflect or with go/types.
type goType interface {
	// id identifies a named type, nil for an unnamed one.
	id() any
	// String names the type as Go writes it with package names, such as myblog.Post.
	String() string
	kind() reflect.Kind
	bits() int
	elem() goType
	key() goType
	numField() int
	field(i int) goField
	is(pkg, name string) bool
	// unmarshals reports whether the type or its pointer has a method json v2 would call
	// instead of reading the value field by field.
	unmarshals() bool
}

type goField struct {
	name               string
	exported, embedded bool
	tag                reflect.StructTag
	typ                goType
	pos                token.Pos
}

// FromReflect returns the tree of the schema type t, a struct that embeds content.Entry.
func FromReflect(t reflect.Type) (*Node, []Problem) {
	return build(reflectType{t})
}

// FromTypes returns the tree of the schema type t, a struct that embeds content.Entry.
func FromTypes(t types.Type) (*Node, []Problem) {
	return build(typesType{t})
}

// build makes the tree of the root struct t, leaving out the embedded content.Entry and checking
// that no key is one Entry has or tifa reads.
func build(t goType) (*Node, []Problem) {
	b := &builder{named: map[any]*Node{}}
	if t.kind() != reflect.Struct {
		b.problem("%s must be a struct type that embeds content.Entry, not a pointer to it", t)
		return nil, b.problems
	}
	root := &Node{Kind: Struct}
	if id := t.id(); id != nil {
		b.named[id] = root
	}
	b.fields(root, t, t.String(), true)
	return root, b.problems
}

type builder struct {
	named    map[any]*Node
	problems []Problem
	pos      token.Pos // of the field being read
}

func (b *builder) problem(format string, args ...any) {
	b.problems = append(b.problems, Problem{Pos: b.pos, Msg: fmt.Sprintf(format, args...)})
}

const holdable = "use a string, number, bool, time.Time, pointer, slice, map with string keys or struct"

// node returns the tree of t, reached through the Go path name. It returns nil after reporting a
// problem.
func (b *builder) node(t goType, name string) *Node {
	if t.is("time", "Time") {
		return &Node{Kind: Time}
	}
	if id := t.id(); id != nil {
		if n, ok := b.named[id]; ok {
			return n
		}
	}
	if t.is("time", "Duration") || t.is("encoding/json", "Number") {
		b.problem("%s has the type %s, which front matter cannot hold: %s", name, t, holdable)
		return nil
	}
	if t.unmarshals() {
		b.problem("%s has the type %s, which reads its own JSON, so front matter cannot be checked against it: %s", name, t, holdable)
		return nil
	}
	switch k := t.kind(); k {
	case reflect.String:
		return &Node{Kind: String}
	case reflect.Bool:
		return &Node{Kind: Bool}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &Node{Kind: Int, Bits: t.bits()}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Node{Kind: Uint, Bits: t.bits()}
	case reflect.Float32, reflect.Float64:
		return &Node{Kind: Float, Bits: t.bits()}
	case reflect.Pointer:
		if e := b.node(t.elem(), name); e != nil {
			return &Node{Kind: Pointer, Elem: e}
		}
		return nil
	case reflect.Slice:
		if e := t.elem(); e.id() == nil && (e.kind() == reflect.Uint8) {
			b.problem("%s is a []byte, which JSON holds as base64 text: %s", name, holdable)
			return nil
		}
		if e := b.node(t.elem(), name+"[]"); e != nil {
			return &Node{Kind: Slice, Elem: e}
		}
		return nil
	case reflect.Map:
		if t.key().kind() != reflect.String || t.key().unmarshals() {
			b.problem("%s has the type %s, and the keys of front matter maps are strings: %s", name, t, holdable)
			return nil
		}
		if e := b.node(t.elem(), name+"[]"); e != nil {
			return &Node{Kind: Map, Elem: e}
		}
		return nil
	case reflect.Struct:
		n := &Node{Kind: Struct}
		if id := t.id(); id != nil {
			b.named[id] = n
		}
		b.fields(n, t, name, false)
		return n
	}
	b.problem("%s has the type %s, which front matter cannot hold: %s", name, t, holdable)
	return nil
}

// fields adds the fields of the struct t to n, those of embedded structs without a json name too.
// root is true for the schema type itself, whose embedded content.Entry is tifa's.
func (b *builder) fields(n *Node, t goType, name string, root bool) {
	byKey := map[string]string{}
	var walk func(t goType, name string)
	walk = func(t goType, name string) {
		for i := range t.numField() {
			f := t.field(i)
			b.pos = f.pos
			goName := name + "." + f.name
			jsonTag, hasJSON := f.tag.Lookup("json")
			if _, ok := f.tag.Lookup("yaml"); ok {
				b.problem("%s has a yaml tag: front matter keys are the json names, remove the yaml tag", goName)
			}
			if jsonTag == "-" {
				continue
			}
			if !f.exported && !f.embedded {
				continue
			}
			key, opts, _ := strings.Cut(jsonTag, ",")
			optional := false
			bad := false
			if opts != "" {
				for o := range strings.SplitSeq(opts, ",") {
					switch o {
					case "omitempty", "omitzero":
						optional = true
					default:
						b.problem("%s has the json option %q: a schema field takes only omitempty and omitzero", goName, o)
						bad = true
					}
				}
			}
			if bad {
				continue
			}
			if f.embedded && key == "" {
				if root && f.typ.is(entryPkg, entryName) {
					continue
				}
				switch {
				case !f.exported:
					b.problem("%s is an unexported embedded field: export its type, or give the field a name", goName)
				case f.typ.kind() == reflect.Pointer:
					b.problem("%s is an embedded pointer: embed the struct itself, or give the field a name", goName)
				case f.typ.kind() != reflect.Struct || f.typ.is("time", "Time") || f.typ.unmarshals():
					b.problem("%s is embedded and is not a struct whose fields front matter holds: give it a json name", goName)
				case hasJSON && opts != "":
					b.problem("%s is embedded with json options: give it a json name", goName)
				default:
					walk(f.typ, goName)
				}
				continue
			}
			if key == "" {
				key = f.name
			}
			if root {
				if why, ok := reserved[key]; ok {
					b.problem("%s has the json name %q, which content.Entry uses %s", goName, key, why)
					continue
				}
				if key == "translationKey" {
					b.problem("%s has the json name \"translationKey\", which tifa reads to link the translations of an entry", goName)
					continue
				}
			}
			if other, ok := byKey[key]; ok {
				b.problem("%s and %s have the same json name %q: rename one or tag it json:\"-\"", other, goName, key)
				continue
			}
			byKey[key] = goName
			child := b.node(f.typ, goName)
			b.pos = f.pos
			if child == nil {
				continue
			}
			k := child.Kind
			n.Fields = append(n.Fields, &Field{
				Key: key, Go: goName, Node: child,
				Required: !optional && k != Pointer && k != Slice && k != Map,
			})
		}
	}
	walk(t, name)
}

// Keys returns the keys of the fields of a Struct, in order.
func (n *Node) Keys() []string {
	keys := make([]string, len(n.Fields))
	for i, f := range n.Fields {
		keys[i] = f.Key
	}
	return keys
}

func (n *Node) field(key string) *Field {
	i := slices.IndexFunc(n.Fields, func(f *Field) bool { return f.Key == key })
	if i < 0 {
		return nil
	}
	return n.Fields[i]
}

// reflectType reads a type with reflect.
type reflectType struct{ t reflect.Type }

func (r reflectType) id() any {
	if r.t.PkgPath() == "" { // unnamed, or predeclared as go/types has it
		return nil
	}
	return r.t
}
func (r reflectType) String() string {
	return strings.ReplaceAll(r.t.String(), "interface {}", "any")
}
func (r reflectType) kind() reflect.Kind { return r.t.Kind() }
func (r reflectType) bits() int {
	switch r.t.Kind() {
	case reflect.Int, reflect.Uint:
		return 64
	}
	return r.t.Bits()
}
func (r reflectType) elem() goType  { return reflectType{r.t.Elem()} }
func (r reflectType) key() goType   { return reflectType{r.t.Key()} }
func (r reflectType) numField() int { return r.t.NumField() }
func (r reflectType) is(pkg, name string) bool {
	return r.t.PkgPath() == pkg && r.t.Name() == name
}
func (r reflectType) field(i int) goField {
	f := r.t.Field(i)
	return goField{name: f.Name, exported: f.IsExported(), embedded: f.Anonymous, tag: f.Tag, typ: reflectType{f.Type}}
}
func (r reflectType) unmarshals() bool {
	p := reflect.PointerTo(r.t)
	for _, m := range unmarshalMethods {
		if _, ok := p.MethodByName(m); ok {
			return true
		}
	}
	return false
}

// unmarshalMethods are the methods json v2 calls to read a value itself.
var unmarshalMethods = []string{"UnmarshalJSON", "UnmarshalJSONFrom", "UnmarshalText"}

// typesType reads a type with go/types.
type typesType struct{ t types.Type }

func (g typesType) under() types.Type { return types.Unalias(g.t).Underlying() }

func (g typesType) id() any {
	if _, ok := types.Unalias(g.t).(*types.Named); ok {
		return types.TypeString(g.t, nil)
	}
	return nil
}
func (g typesType) String() string {
	// Without aliases, as reflect names types.
	return strings.ReplaceAll(types.TypeString(types.Unalias(g.t), func(p *types.Package) string { return p.Name() }), "interface{}", "any")
}
func (g typesType) kind() reflect.Kind {
	switch u := g.under().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.String:
			return reflect.String
		case types.Bool:
			return reflect.Bool
		case types.Int:
			return reflect.Int
		case types.Int8:
			return reflect.Int8
		case types.Int16:
			return reflect.Int16
		case types.Int32:
			return reflect.Int32
		case types.Int64:
			return reflect.Int64
		case types.Uint:
			return reflect.Uint
		case types.Uint8:
			return reflect.Uint8
		case types.Uint16:
			return reflect.Uint16
		case types.Uint32:
			return reflect.Uint32
		case types.Uint64:
			return reflect.Uint64
		case types.Float32:
			return reflect.Float32
		case types.Float64:
			return reflect.Float64
		}
	case *types.Pointer:
		return reflect.Pointer
	case *types.Slice:
		return reflect.Slice
	case *types.Map:
		return reflect.Map
	case *types.Struct:
		return reflect.Struct
	}
	return reflect.Invalid
}
func (g typesType) bits() int {
	switch g.under().(*types.Basic).Kind() {
	case types.Int8, types.Uint8:
		return 8
	case types.Int16, types.Uint16:
		return 16
	case types.Int32, types.Uint32, types.Float32:
		return 32
	}
	return 64
}
func (g typesType) elem() goType {
	switch u := g.under().(type) {
	case *types.Pointer:
		return typesType{u.Elem()}
	case *types.Slice:
		return typesType{u.Elem()}
	case *types.Map:
		return typesType{u.Elem()}
	}
	panic("elem of " + g.String())
}
func (g typesType) key() goType   { return typesType{g.under().(*types.Map).Key()} }
func (g typesType) numField() int { return g.under().(*types.Struct).NumFields() }
func (g typesType) field(i int) goField {
	s := g.under().(*types.Struct)
	f := s.Field(i)
	return goField{name: f.Name(), exported: f.Exported(), embedded: f.Embedded(), tag: reflect.StructTag(s.Tag(i)), typ: typesType{f.Type()}, pos: f.Pos()}
}
func (g typesType) is(pkg, name string) bool {
	n, ok := types.Unalias(g.t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}
func (g typesType) unmarshals() bool {
	ms := types.NewMethodSet(types.NewPointer(g.t))
	for _, m := range unmarshalMethods {
		if ms.Lookup(nil, m) != nil {
			return true
		}
	}
	return false
}
