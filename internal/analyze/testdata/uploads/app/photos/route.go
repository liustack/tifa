package photos

import (
	"mime/multipart"

	"github.com/liustack/tifa"

	"example.com/uploads"
)

var (
	BodyLimit   = uploads.UploadLimit
	ReadTimeout = uploads.UploadTime
)

type Photo struct {
	ID string `json:"id"`
}

func Load(c *tifa.Ctx) ([]Photo, error) { return nil, nil }

// UploadInput is a title and one picture.
type UploadInput struct {
	Title string                `json:"title"`
	Tags  []string              `json:"tags"`
	Photo *multipart.FileHeader `json:"photo"`
	Draft bool                  `json:"draft,omitempty"`
}

func POST(c *tifa.Ctx, in UploadInput) (Photo, error) { return Photo{}, nil }

// AlbumInput is several pictures at once, one of them optional.
type AlbumInput struct {
	Name   string                  `json:"name"`
	Photos []*multipart.FileHeader `json:"photos"`
	Cover  *multipart.FileHeader   `json:"cover,omitempty"`
}

func PUT(c *tifa.Ctx, in AlbumInput) error { return nil }

// RenameInput holds no file, so it stays JSON.
type RenameInput struct {
	Title string `json:"title"`
}

func PATCH(c *tifa.Ctx, in RenameInput) error { return nil }
