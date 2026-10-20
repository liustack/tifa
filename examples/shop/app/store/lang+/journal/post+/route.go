// Package post is the page of one journal post.
package post

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/journal"
)

// Article is a post with the others to read next.
type Article struct {
	Post journal.Post      `json:"post"`
	More []journal.Summary `json:"more"`
}

func Load(c *tifa.Ctx) (Article, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return Article{}, err
	}
	p, err := journal.GetPost(c.Param("post"), l)
	if err != nil {
		return Article{}, err
	}
	a := Article{Post: p, More: []journal.Summary{}}
	for _, s := range journal.List(l) {
		if s.Slug != p.Slug {
			a.More = append(a.More, s)
		}
	}
	return a, nil
}
