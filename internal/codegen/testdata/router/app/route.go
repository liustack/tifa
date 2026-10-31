package app

import "github.com/liustack/tifa"

type Site struct {
	Name string `json:"name"`
}

func Layout(c *tifa.Ctx) (Site, error) { return Site{Name: "router"}, nil }
