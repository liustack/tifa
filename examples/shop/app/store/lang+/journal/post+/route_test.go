package post

import (
	"testing"

	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/journal"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

func TestEveryPostShowsProductsOfTheCatalogue(t *testing.T) {
	l, _ := store.FindLocale("zh-CN")
	for _, p := range journal.List(l) {
		if len(shown[p.Slug]) == 0 {
			t.Errorf("%s shows no products", p.Slug)
		}
		for _, slug := range shown[p.Slug] {
			if _, err := products.GetProduct(slug, l); err != nil {
				t.Errorf("%s shows %s: %v", p.Slug, slug, err)
			}
		}
	}
}
