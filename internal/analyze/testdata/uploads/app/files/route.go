package files

import (
	"mime/multipart"
	"time"

	"github.com/liustack/tifa"
)

const (
	BodyLimit   = 64 << 20
	ReadTimeout = 10 * time.Minute
)

type Input struct {
	File *multipart.FileHeader `json:"file"`
}

func POST(c *tifa.Ctx, in Input) error { return nil }
