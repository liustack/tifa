package photos

import (
	"mime/multipart"
	"time"

	"github.com/liustack/tifa"
)

// The photos of an order may be larger and slower than other requests.
const BodyLimit = 8 << 20

var ReadTimeout = 2 * time.Minute

type Photo struct {
	Name string `json:"name"`
}

type UploadInput struct {
	Caption string                `json:"caption"`
	Photo   *multipart.FileHeader `json:"photo"`
}

func POST(c *tifa.Ctx, in UploadInput) (Photo, error) { return Photo{Name: in.Photo.Filename}, nil }

type AlbumInput struct {
	Photos []*multipart.FileHeader `json:"photos"`
}

func PUT(c *tifa.Ctx, in AlbumInput) error { return nil }
