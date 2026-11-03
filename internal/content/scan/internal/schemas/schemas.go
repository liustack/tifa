// Package schemas holds schema types for the test that compares the field trees reflect and
// go/types give. Each type is read both ways: from the compiled package and from its source.
package schemas

import (
	"encoding/json"
	"encoding/json/jsontext"
	"time"

	"github.com/liustack/tifa/content"
)

// All lists a value of each schema type by its name.
var All = map[string]any{
	"Basic":          Basic{},
	"Nested":         Nested{},
	"Embedding":      Embedding{},
	"Recursive":      Recursive{},
	"RecursiveNamed": RecursiveNamed{},
	"SelfPointer":    SelfPointer{},
	"Optional":       Optional{},
	"Named":          Named{},
	"BadTags":        BadTags{},
	"BadTypes":       BadTypes{},
	"BadEmbedding":   BadEmbedding{},
	"Reserved":       Reserved{},
	"Clash":          Clash{},
	"NoEntry":        NoEntry{},
	"TwiceEntry":     TwiceEntry{},
	"DeepEntry":      DeepEntry{},
	"RootJSON":       RootJSON{},
	"RootText":       RootText{},
	"RootFrom":       RootFrom{},
}

type Basic struct {
	content.Entry
	S   string  `json:"s"`
	B   bool    `json:"b"`
	I   int     `json:"i"`
	I8  int8    `json:"i8"`
	I16 int16   `json:"i16"`
	I32 int32   `json:"i32"`
	I64 int64   `json:"i64"`
	U   uint    `json:"u"`
	U8  uint8   `json:"u8"`
	U16 uint16  `json:"u16"`
	U32 uint32  `json:"u32"`
	U64 uint64  `json:"u64"`
	F32 float32 `json:"f32"`
	F64 float64 `json:"f64"`
	T   time.Time
}

type Image struct {
	Src   string `json:"src"`
	Width int    `json:"width,omitempty"`
}

type Nested struct {
	content.Entry
	Image   Image            `json:"image"`
	Cover   *Image           `json:"cover"`
	Gallery []Image          `json:"gallery"`
	ByName  map[string]Image `json:"byName"`
	Lists   [][]string       `json:"lists"`
	Inline  struct {
		A string `json:"a"`
	} `json:"inline"`
}

type Meta struct {
	Author string `json:"author"`
	Note   string `json:"note,omitempty"`
}

type Embedding struct {
	content.Entry
	Meta
	Named Meta `json:"meta"`
	Title string
}

type Node struct {
	Name     string `json:"name"`
	Children []Node `json:"children"`
	Parent   *Node  `json:"parent"`
}

type Recursive struct {
	content.Entry
	Root Node `json:"root"`
}

type (
	Nodes []Nodes
	Tree  map[string]Tree
	Loop  *Loop
	Ring  []*Ring
)

type RecursiveNamed struct {
	content.Entry
	Nodes Nodes `json:"nodes"`
	Tree  Tree  `json:"tree"`
	Ring  Ring  `json:"ring"`
	Again Nodes `json:"again"`
}

type SelfPointer struct {
	content.Entry
	Loop Loop `json:"loop"`
}

type Optional struct {
	content.Entry
	A       string `json:"a,omitempty"`
	B       int    `json:"b,omitzero"`
	C       *int   `json:"c"`
	D       string `json:"-"`
	E       string `json:"-,"`
	private string
}

type Status string

type Count int16

type Named struct {
	content.Entry
	Status Status            `json:"status"`
	Count  Count             `json:"count"`
	Tags   map[Status]Count  `json:"tags"`
	Old    *time.Time        `json:"old"`
	Times  []time.Time       `json:"times"`
	Raw    json.RawMessage   `json:"raw,omitempty"`
	Number json.Number       `json:"number,omitempty"`
	ByTime map[string]Status `json:"byTime"`
}

type BadTags struct {
	content.Entry
	Y string `json:"y" yaml:"why"`
	Q int    `json:"q,string"`
	C string `json:"c,case:ignore"`
}

type Text struct{ v string }

func (t *Text) UnmarshalText(b []byte) error { t.v = string(b); return nil }

type BadTypes struct {
	content.Entry
	Ch   chan int       `json:"ch"`
	Any  any            `json:"any"`
	Fn   func()         `json:"fn"`
	Raw  []byte         `json:"raw"`
	Arr  [2]string      `json:"arr"`
	IntK map[int]string `json:"intK"`
	Txt  Text           `json:"txt"`
	C    complex64      `json:"c"`
	D    time.Duration  `json:"d"`
}

type hidden struct{ X string }

type BadEmbedding struct {
	content.Entry
	*Image
	hidden
	Status
}

type Reserved struct {
	content.Entry
	Date  time.Time `json:"date"`
	Key   string    `json:"translationKey"`
	ID    string
	Words int `json:"words,omitempty"`
}

type Clash struct {
	content.Entry
	Meta
	Author string `json:"author"`
}

type E = content.Entry

type TwiceEntry struct {
	content.Entry
	E     `json:"-"`
	Title string `json:"title"`
}

type WithEntry struct {
	content.Entry
	Author string `json:"author"`
}

type DeepEntry struct {
	content.Entry
	WithEntry
}

type RootJSON struct {
	content.Entry
	Title string `json:"title"`
}

func (RootJSON) UnmarshalJSON([]byte) error { return nil }

type RootText struct {
	content.Entry
	Title string `json:"title"`
}

func (*RootText) UnmarshalText([]byte) error { return nil }

type RootFrom struct {
	content.Entry
	Title string `json:"title"`
}

func (*RootFrom) UnmarshalJSONFrom(*jsontext.Decoder) error { return nil }

type NoEntry struct {
	Title string `json:"title"`
}
