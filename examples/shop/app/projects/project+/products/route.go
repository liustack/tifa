// Package products is the product list of the back office. The studio shop sells the products of
// the storefront, so its list is the catalogue of the storefront, with prices and stock. The weekend
// shop lists its own.
package products

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
	catalogue "github.com/liustack/tifa/examples/shop/app/store/products"
)

// Load lists the products of the shop, in the order of its catalogue.
func Load(c *tifa.Ctx) ([]catalogue.Entry, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return nil, err
	}
	return orders.ShopEntries(p.ID)
}
