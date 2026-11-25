package api

import "github.com/liustack/tifa"

// The directory is the URL.

type Input struct {
	Tifa string `json:"_tifa"`
}

func GET(c *tifa.Ctx, in Input) error { return nil }
