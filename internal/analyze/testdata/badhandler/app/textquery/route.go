package textquery

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

// Tags reads and writes itself as one string, which a query string or a form does not call: it
// sends "a,b" as one value, and the slice would get it whole.
type Tags []string

func (t Tags) MarshalText() ([]byte, error)  { return nil, nil }
func (t *Tags) UnmarshalText(b []byte) error { return nil }

// IDs is one string too, which a slice of numbers would not parse.
type IDs []int

func (ids IDs) MarshalText() ([]byte, error)  { return nil, nil }
func (ids *IDs) UnmarshalText(b []byte) error { return nil }

// Level is a string in a slice, read by its pointer.
type Level int

func (l Level) MarshalText() ([]byte, error)  { return nil, nil }
func (l *Level) UnmarshalText(b []byte) error { return nil }

type Query struct {
	Tags   Tags    `json:"tags"`
	IDs    IDs     `json:"ids"`
	Levels []Level `json:"levels"`
	Plain  []int   `json:"plain"`
}

func GET(c *tifa.Ctx, in Query) error { return nil }

func DELETE(c *tifa.Ctx, in Query) error { return nil }

type Upload struct {
	Tags Tags                  `json:"tags"`
	File *multipart.FileHeader `json:"file"`
}

func POST(c *tifa.Ctx, in Upload) error { return nil }
