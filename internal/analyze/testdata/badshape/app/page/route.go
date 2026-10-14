package page

import (
	"encoding/json"
	"time"

	"github.com/liustack/tifa"
)

// The directory is the URL.

type encoder struct{} // stands in for jsontext.Encoder, absent under GOEXPERIMENT=nojsonv2

// Both is written by MarshalJSONTo under json v2 and by MarshalText under json v1.
type Both struct{}

func (*Both) MarshalText() ([]byte, error)   { return []byte("v1"), nil }
func (*Both) MarshalJSONTo(e *encoder) error { return nil }

// AppendByte is a byte that json v2 writes through AppendText.
type AppendByte byte

func (AppendByte) AppendText(b []byte) ([]byte, error) { return append(b, "v2"...), nil }

// PlainByte is a named byte without methods.
type PlainByte byte

// Text is an interface used as a map key.
type Text interface{ MarshalText() ([]byte, error) }

// WrongTo has a MarshalJSONTo that neither implementation calls.
type WrongTo struct{ X int }

func (WrongTo) MarshalJSONTo() string { return "" }

type hidden struct{ X int }

func (hidden) MarshalText() ([]byte, error) { return []byte("text"), nil }

// Shadow's own MarshalText has the wrong signature and hides hidden's.
type Shadow struct {
	hidden `json:"h"`
}

func (Shadow) MarshalText() string { return "" }

// Promoted gets MarshalText from an embedded field.
type Promoted struct{ Inner }

type Inner struct{}

func (Inner) MarshalText() ([]byte, error) { return []byte("i"), nil }

// PtrText declares MarshalText on its pointer.
type PtrText struct{}

func (*PtrText) MarshalText() ([]byte, error) { return []byte("p"), nil }

// Key is a string kind with MarshalText, which json v1 ignores for map keys.
type Key string

func (Key) MarshalText() ([]byte, error) { return []byte("k"), nil }

// Appender has a method named AppendJSON.
type Appender struct{}

func (Appender) AppendJSON(b []byte) []byte { return b }

// Stamp gets time.Time's MarshalJSON by embedding.
type Stamp struct{ time.Time }

type Status string

type Data struct {
	Both    *Both            `json:"both"`
	Bytes   []AppendByte     `json:"bytes"`
	Plain   []PlainByte      `json:"plain"`
	Embed   struct{ X int }  `json:",embed"`
	Hex     int              `json:"hex,format:hex"`
	Oops    int              `json:"oops,string:oops"`
	Unknown int              `json:"unknown,inline"`
	Comma   int              `json:"comma,"`
	Twice   int              `json:"twice,omitempty,omitempty"`
	Named   Status           `json:"named,string"`
	ByText  map[Text]int     `json:"byText"`
	Wrong   WrongTo          `json:"wrong"`
	Shadow  Shadow           `json:"shadow"`
	Prom    Promoted         `json:"prom"`
	Ptr     PtrText          `json:"ptr"`
	ByKey   map[Key]int      `json:"byKey"`
	App     Appender         `json:"app"`
	Stamp   Stamp            `json:"stamp"`
	Num     json.Number      `json:"num,string"`
	ByPtr   map[*PtrText]int `json:"byPtr"`
	secret
}

type secret struct{ Y int }

func Load(c *tifa.Ctx) (Data, error) { return Data{}, nil }
