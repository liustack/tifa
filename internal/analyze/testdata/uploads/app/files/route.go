package files

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

type Input struct {
	File *multipart.FileHeader `json:"file"`
}

func POST(c *tifa.Ctx, in Input) error { return nil }
