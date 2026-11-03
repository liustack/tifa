// Package path is a page of the docs, /{lang}/docs and /{lang}/docs/{path...}, with the side bar.
package path

import (
	"cmp"
	"slices"

	"github.com/liustack/tifa"

	"example.com/contenttest"
	"example.com/contenttest/app/lang+"
)

type NavItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Page struct {
	Doc  contenttest.Doc `json:"doc"`
	Body tifa.HTML       `json:"body"`
	Nav  []NavItem       `json:"nav"`
}

func Load(c *tifa.Ctx) (Page, error) {
	s, err := tifa.Use(c, lang.Layout)
	if err != nil {
		return Page{}, err
	}
	d, err := contenttest.Docs.Get(s.Lang, cmp.Or(c.Param("path"), "index")) // /en/docs is index.md
	if err != nil {
		return Page{}, err
	}
	docs := contenttest.Docs.In(s.Lang)
	slices.SortStableFunc(docs, func(a, b contenttest.Doc) int { return cmp.Compare(a.Order, b.Order) }) // ID order for the same Order
	nav := make([]NavItem, 0, len(docs))
	for _, x := range docs {
		nav = append(nav, NavItem{ID: x.ID, Title: x.Title})
	}
	return Page{Doc: d, Body: d.Body(), Nav: nav}, nil
}
