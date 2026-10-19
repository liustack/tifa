package lang

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/journal"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

// Home is the storefront's home: new products, the categories and the latest journal posts.
type Home struct {
	Featured []products.Card   `json:"featured"`
	Posts    []journal.Summary `json:"posts"`
	Sale     []products.Card   `json:"sale"`
}

func Load(c *tifa.Ctx) (Home, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return Home{}, err
	}
	sale := products.Search(products.Filter{Query: "", Sort: "price-desc"}, l).Products
	var onSale []products.Card
	for _, p := range sale {
		if p.CompareAtCents > 0 {
			onSale = append(onSale, p)
		}
	}
	return Home{Featured: products.Featured(8, l), Posts: journal.List(l)[:3], Sale: onSale}, nil
}
