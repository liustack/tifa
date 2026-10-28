// Package post is the page of one journal post.
package post

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/journal"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

// Article is a post with the products it shows and the others to read next.
type Article struct {
	Post     journal.Post      `json:"post"`
	Products []products.Card   `json:"products"`
	More     []journal.Summary `json:"more"`
}

// shown are the products each post shows in its photos.
var shown = map[string][]string{
	"kettle-photos":  {"pour-over-kettle", "ceramic-cup", "oak-tray", "tea-canister"},
	"stock-sync-api": {"oak-tray", "canvas-tote", "dot-notebook", "enamel-mug"},
	"theme-css":      {"walnut-pen", "linen-coasters", "stoneware-vase", "reading-candle"},
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
	a := Article{Post: p, Products: []products.Card{}, More: []journal.Summary{}}
	for _, slug := range shown[p.Slug] {
		product, err := products.GetProduct(slug, l)
		if err != nil {
			return Article{}, err
		}
		a.Products = append(a.Products, product.Card)
	}
	for _, s := range journal.List(l) {
		if s.Slug != p.Slug {
			a.More = append(a.More, s)
		}
	}
	return a, nil
}
