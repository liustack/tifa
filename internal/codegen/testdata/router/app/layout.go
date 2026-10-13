package app

import "github.com/liustack/tifa"

type Site struct {
	Name string `json:"name"`
}

func Load(c *tifa.Ctx) (Site, error) { return Site{Name: "router"}, nil }
