// Package app is the root Layout: it renders the whole document.
package app

import "github.com/liustack/tifa"

type Site struct {
	Name string `json:"name"`
	Lang string `json:"lang"`
}

func Layout(c *tifa.Ctx) (Site, error) {
	lang := c.Query("lang")
	if lang == "" {
		lang = "en"
	}
	return Site{Name: "Pages", Lang: lang}, nil
}
