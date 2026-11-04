package result

import (
	"mime/multipart"

	"github.com/liustack/tifa"
)

type Data struct {
	File *multipart.FileHeader `json:"file"`
}

func Load(c *tifa.Ctx) (Data, error) { return Data{}, nil }

func POST(c *tifa.Ctx) (*multipart.FileHeader, error) { return nil, nil }
