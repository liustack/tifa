package app

import (
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (string, error) { return c.Query("ref"), nil }

type Site struct {
	Name string `json:"name"`
	Lang string `json:"lang"`
}

func Layout(c *tifa.Ctx) (Site, error) { return Site{Name: "shop", Lang: c.Query("lang")}, nil }
