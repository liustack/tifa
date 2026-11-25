// Package app is the root Layout: it renders the whole document.
package app

import "github.com/liustack/tifa"

type Site struct {
	Name string `json:"name"`
	Lang string `json:"lang"`
}

// Query is the query string of every page.
type Query struct {
	Lang string `json:"lang"`
}

func Layout(c *tifa.Ctx, q Query) (Site, error) {
	lang := q.Lang
	if lang == "" {
		lang = "en"
	}
	return Site{Name: "Pages", Lang: lang}, nil
}
