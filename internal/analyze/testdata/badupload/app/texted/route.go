package texted

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

// Files reads and writes itself as text, which a file of a form cannot be.
type Files []*multipart.FileHeader

func (Files) MarshalText() ([]byte, error) { return nil, nil }
func (*Files) UnmarshalText([]byte) error  { return nil }

type Input struct {
	Files Files `json:"files"`
}

func POST(c *tifa.Ctx, in Input) error { return nil }
