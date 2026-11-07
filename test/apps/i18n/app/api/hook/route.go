// Package hook is an API route outside every area.
package hook

import "github.com/liustack/tifa"

type Out struct {
	Language string `json:"language"`
}

func GET(c *tifa.Ctx) (Out, error) { return Out{Language: c.Language()}, nil }
