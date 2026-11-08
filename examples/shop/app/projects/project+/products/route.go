// Package products is the product list of the back office. The studio shop sells the products of
// the storefront, so its list is the catalogue of the storefront, with prices and stock. The other
// shop has no such list.
package products

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
	catalogue "github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// Load lists the products of the shop, in the order of the catalogue.
func Load(c *tifa.Ctx) ([]catalogue.Entry, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return nil, err
	}
	if p.ID != storekit.Project {
		return nil, tifa.ErrNotFound
	}
	return catalogue.Entries(), nil
}
