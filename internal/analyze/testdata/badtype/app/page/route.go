package page

import (
	"encoding/json"

	"github.com/liustack/tifa"
)

// The directory is the URL.

type Data struct {
	Name    string             `json:"name"`
	Updates chan int           `json:"updates"`
	ByScore map[float64]string `json:"byScore"`
	Items   []Item             `json:"items"`
	Raw     json.RawMessage    `json:"raw"`
	Bytes   []B                `json:"bytes"`
	Ptrs    []PtrMarshal       `json:"ptrs"`
	Text    TextValue          `json:"text"`
	Bad     int                `json:"bad'name"`
	Append  AppendOnly         `json:"append"`
	Stream  Streamer           `json:"stream"`
}

type encoder struct{} // stands in for jsontext.Encoder, absent under GOEXPERIMENT=nojsonv2

// Streamer writes itself through json v2's MarshalJSONTo, which json v1 ignores.
type Streamer struct{}

func (Streamer) MarshalJSONTo(*encoder) error { return nil }

// AppendOnly is written by AppendText under json v2 and as a number under json v1.
type AppendOnly int

func (AppendOnly) AppendText(b []byte) ([]byte, error) { return append(b, "x"...), nil }

// TextValue is an interface: the dynamic value may also implement MarshalJSON, or be nil.
type TextValue interface{ MarshalText() ([]byte, error) }

type Item struct {
	Extra any `json:"extra"`
}

// B is a byte with its own MarshalJSON, so []B is not base64.
type B byte

func (B) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

// PtrMarshal marshals itself through a pointer receiver, which applies to addressable values only.
type PtrMarshal struct {
	V int `json:"v"`
}

func (*PtrMarshal) MarshalJSON() ([]byte, error) { return []byte(`"p"`), nil }

func Load(c *tifa.Ctx) (Data, error) { return Data{}, nil }
