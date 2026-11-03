package api

import "github.com/liustack/tifa"

type Preview struct {
	HTML tifa.HTML `json:"html"`
}

func POST(c *tifa.Ctx) (Preview, error) { return Preview{}, nil }
