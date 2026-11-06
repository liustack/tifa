// Package projects is a page outside every area: it has no language and is never prefixed.
package projects

import "github.com/liustack/tifa"

type Data struct {
	Language string `json:"language"`
}

func Load(c *tifa.Ctx) (Data, error) { return Data{Language: c.Language()}, nil }
