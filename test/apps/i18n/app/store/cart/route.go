// Package cart is a page of the area app/store.
package cart

import "github.com/liustack/tifa"

type Data struct {
	Language string `json:"language"`
}

func Load(c *tifa.Ctx) (Data, error) { return Data{Language: c.Language()}, nil }
