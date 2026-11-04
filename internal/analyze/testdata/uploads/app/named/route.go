package named

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

// Files and Photo are named types of files: by their underlying types they are files too.
type (
	Files []*multipart.FileHeader
	Photo *multipart.FileHeader
)

type Input struct {
	Title  string  `json:"title"`
	Files  Files   `json:"files"`
	Cover  Photo   `json:"cover"`
	Extras []Photo `json:"extras"`
}

func POST(c *tifa.Ctx, in Input) error { return nil }
