package file

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

type Filter struct {
	Photo *multipart.FileHeader `json:"photo"`
}

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
