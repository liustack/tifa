// Package app is the root Layout: what it loads is the same for every visitor.
package app

import "github.com/liustack/tifa"

type Site struct {
	Name string `json:"name"`
}

func Layout(c *tifa.Ctx) (Site, error) { return Site{Name: "Sessions"}, nil }
