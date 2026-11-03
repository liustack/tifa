package page

import (
	"encoding/json"
	"time"

	"github.com/liustack/tifa"
)

// The directory is the URL.

type encoder struct{} // stands in for jsontext.Encoder

// Both has MarshalJSONTo and MarshalText.
type Both struct{}

func (*Both) MarshalText() ([]byte, error)   { return []byte("v1"), nil }
func (*Both) MarshalJSONTo(e *encoder) error { return nil }

// AppendByte is a byte that json v2 writes through AppendText.
type AppendByte byte

func (AppendByte) AppendText(b []byte) ([]byte, error) { return append(b, "v2"...), nil }

// Text is an interface used as a map key.
type Text interface{ MarshalText() ([]byte, error) }

// WrongTo has a MarshalJSONTo that json v2 does not call.
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

// Appender has a method named AppendJSON.
type Appender struct{}

func (Appender) AppendJSON(b []byte) []byte { return b }

// Stamp gets time.Time's MarshalJSON by embedding.
type Stamp struct{ time.Time }

type Status string

type Data struct {
	Both    *Both            `json:"both"`
	Bytes   []AppendByte     `json:"bytes"`
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
	App     Appender         `json:"app"`
	Stamp   Stamp            `json:"stamp"`
	Num     json.Number      `json:"num,string"`
	ByPtr   map[*PtrText]int `json:"byPtr"`
	secret
	Dur    time.Duration `json:"dur"`
	Flag   bool          `json:"flag,string"`
	Text   string        `json:"text,string"`
	Dup1   int           `json:"dup"`
	Dup2   int           `json:"dup"`
	tagged int           `json:"tagged"`
	Locked Opaque        `json:"locked"`
	Wrap   Wrapper       `json:"wrap"`
	Reads  Reader        `json:"reads"`
	Opts   WithOpts      `json:"opts"`
}

// Opaque has fields but none json v2 can write.
type Opaque struct{ a int }

// Wrapper embeds a type that is not a struct without a json name.
type Wrapper struct{ Status }

// Reader embeds a struct that json v2 could read with UnmarshalText.
type Reader struct{ Parsed }

type Parsed struct{ X int }

func (*Parsed) UnmarshalText([]byte) error { return nil }

// WithOpts embeds a struct with a tag option.
type WithOpts struct {
	Extra `json:",omitempty"`
}

type Extra struct{ Y int }

type secret struct{ Y int }

func Load(c *tifa.Ctx) (Data, error) { return Data{}, nil }
