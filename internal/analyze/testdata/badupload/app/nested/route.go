package nested

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

type Inner struct {
	File *multipart.FileHeader `json:"file"`
}

type Deep struct {
	Inner Inner `json:"inner"`
}

type Value struct {
	File multipart.FileHeader `json:"file"`
}

type Mapped struct {
	Files map[string]*multipart.FileHeader `json:"files"`
}

func POST(c *tifa.Ctx, in Deep) error { return nil }

func PUT(c *tifa.Ctx, in Value) error { return nil }

func PATCH(c *tifa.Ctx, in Mapped) error { return nil }
