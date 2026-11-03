package api

import "github.com/liustack/tifa"

type Input struct {
	Body tifa.HTML `json:"body"`
}

func POST(c *tifa.Ctx, in Input) error { return nil }
