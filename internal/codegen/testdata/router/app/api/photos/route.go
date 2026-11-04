package photos

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

type Photo struct {
	Name string `json:"name"`
}

// UploadInput holds files, so the generated function sends it as a multipart form.
type UploadInput struct {
	Title  string                  `json:"title"`
	Tags   []string                `json:"tags"`
	Rating int                     `json:"rating"`
	Draft  bool                    `json:"draft"`
	Photo  *multipart.FileHeader   `json:"photo"`
	Extras []*multipart.FileHeader `json:"extras"`
	Cover  *multipart.FileHeader   `json:"cover,omitempty"`
}

func POST(c *tifa.Ctx, in UploadInput) (Photo, error) { return Photo{}, nil }
