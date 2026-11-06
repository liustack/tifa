// Package csr is a page of the area app/store that the server does not render.
package csr

import "github.com/liustack/tifa"

const SSR = false

type Data struct {
	Language string `json:"language"`
}

func Load(c *tifa.Ctx) (Data, error) { return Data{Language: c.Language()}, nil }
