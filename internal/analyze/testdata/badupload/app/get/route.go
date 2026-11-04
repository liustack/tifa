package get

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

type Input struct {
	File *multipart.FileHeader `json:"file"`
}

func GET(c *tifa.Ctx, in Input) error { return nil }

func DELETE(c *tifa.Ctx, in Input) error { return nil }
