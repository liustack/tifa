package app

import "github.com/liustack/tifa"

type Site struct {
	Name string `json:"name"`
	Lang string `json:"lang"`
}

func Layout(c *tifa.Ctx) (Site, error) { return Site{Name: "shop", Lang: c.Query("lang")}, nil }
