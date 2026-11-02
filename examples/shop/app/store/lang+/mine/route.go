// Package mine is what the storefront knows about the visitor in front of it: the cart and the
// products they looked at last. The storefront pages are the same for everyone and are cached,
// so the browser asks for this after a page has loaded.
package mine

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/cart"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

// Mine is the visitor's cart and the products they looked at last, newest first.
type Mine struct {
	Cart cart.Cart       `json:"cart"`
	Seen []products.Card `json:"seen"`
}

// GET answers for the visitor of the cookies it is sent with.
func GET(c *tifa.Ctx) (Mine, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return Mine{}, err
	}
	return Mine{Cart: cart.Get(cart.ID(c), l), Seen: append([]products.Card{}, products.Seen(c, "", l)...)}, nil
}

// SeenInput is the product page the visitor opened.
type SeenInput struct {
	Slug string `json:"slug"`
}

// POST remembers that the visitor looked at a product. A slug the catalogue does not have is
// left out.
func POST(c *tifa.Ctx, in SeenInput) error {
	products.MarkSeen(c, in.Slug)
	return nil
}
