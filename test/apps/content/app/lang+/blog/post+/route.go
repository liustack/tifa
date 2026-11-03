// Package post is a post of the blog, /{lang}/blog/{post}, with its body and table of contents.
package post

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/content"

	"example.com/contenttest"
	"example.com/contenttest/app/lang+"
	"example.com/contenttest/app/lang+/blog"
)

type Page struct {
	Post     contenttest.Post  `json:"post"`
	Body     tifa.HTML         `json:"body"`
	Headings []content.Heading `json:"headings"`
}

func Load(c *tifa.Ctx) (Page, error) {
	s, err := tifa.Use(c, lang.Layout)
	if err != nil {
		return Page{}, err
	}
	p, err := blog.BySlug(s.Lang, c.Param("post"))
	if err != nil {
		return Page{}, err
	}
	return Page{Post: p, Body: p.Body(), Headings: p.Headings()}, nil
}
