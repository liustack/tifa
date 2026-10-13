package page

import (
	"encoding/json"

	"github.com/liustack/tifa"
)

const Path = "/page"

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
}

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
