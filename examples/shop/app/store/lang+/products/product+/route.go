// Package product is the product page of the storefront: the product with its colours and sizes,
// reviews and related products. Its POST puts the product into the cart.
package product

import (
	"strconv"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/cart"
	"github.com/liustack/tifa/examples/shop/app/store/lang+/products"
)

// Detail is the product page.
type Detail struct {
	Product products.Product     `json:"product"`
	Reviews products.ReviewsPage `json:"reviews"`
	Related []products.Card      `json:"related"`
	// Bundle are two products of the same category that go with this one, sold together.
	Bundle    []products.Card     `json:"bundle"`
	Questions []products.Question `json:"questions"`
	// Best are the best rated products of the category, and Seen what the visitor looked at last.
	Best []products.Card `json:"best"`
	Seen []products.Card `json:"seen"`
	// Delivery is when standard delivery would arrive if ordered now.
	Delivery cart.Delivery `json:"delivery"`
}

func Load(c *tifa.Ctx) (Detail, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return Detail{}, err
	}
	p, err := products.GetProduct(c.Param("product"), l)
	if err != nil {
		return Detail{}, err
	}
	seen := products.Seen(c, p.Slug, l)
	products.MarkSeen(c, p.Slug)
	page, _ := strconv.Atoi(c.Query("reviews"))
	d := Detail{
		Product: p, Reviews: products.ReviewPage(p.Slug, page, l), Bundle: products.Bundle(p.Slug, 2, l), Related: products.Related(p.Slug, 12, l),
		Questions: products.Questions(p.Slug, l), Best: products.BestInCategory(p.Slug, 5, l), Seen: seen,
	}
	d.Delivery = cart.Deliveries(cart.Cart{FreeShippingCents: 1}, time.Now(), l)[0]
	return d, nil
}

type AddInput struct {
	Color    string `json:"color"`
	Size     string `json:"size"`
	Quantity int    `json:"quantity"`
}

// Added is the cart after a product went in.
type Added struct {
	Count int `json:"count"`
}

// POST puts the product into the cart and answers with the number of pieces in it.
func POST(c *tifa.Ctx, in AddInput) (Added, error) {
	id, err := cart.Add(c, c.Param("product"), in.Color, in.Size, in.Quantity)
	if err != nil {
		return Added{}, err
	}
	return Added{Count: cart.Count(id)}, nil
}
