package mixed

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

type Mixed struct {
	File *multipart.FileHeader `json:"file"`
	Meta Meta                  `json:"meta"`
}

type Meta struct {
	Name string `json:"name"`
}

func POST(c *tifa.Ctx, in Mixed) error { return nil }
