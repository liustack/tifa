package app

import (
	"github.com/liustack/tifa"
)

// Ref is the query string of the home page.
type Ref struct {
	Ref string `json:"ref"`
}

func Load(c *tifa.Ctx, q Ref) (string, error) { return q.Ref, nil }

type Site struct {
	Name string `json:"name"`
	Lang string `json:"lang"`
}

func Layout(c *tifa.Ctx) (Site, error) { return Site{Name: "shop", Lang: "en"}, nil }
