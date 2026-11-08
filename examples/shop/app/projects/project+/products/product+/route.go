// Package product is the edit page of a product in the back office: staff change its price, stock,
// title and description, and the storefront shows the change on its next request.
package product

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
	catalogue "github.com/liustack/tifa/examples/shop/app/store/products"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// Load reads the product of the URL, in the shop of the storefront only.
func Load(c *tifa.Ctx) (catalogue.Entry, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return catalogue.Entry{}, err
	}
	if p.ID != storekit.Project {
		return catalogue.Entry{}, tifa.ErrNotFound
	}
	return catalogue.GetEntry(c.Param("product"))
}

// PATCH changes the product to what the form says and answers with it. The edit checks itself
// with Validate before it gets here.
func PATCH(c *tifa.Ctx, in catalogue.Edit) (catalogue.Entry, error) {
	if c.Param("project") != storekit.Project {
		return catalogue.Entry{}, tifa.ErrNotFound
	}
	if err := catalogue.Update(c.Param("product"), in); err != nil {
		return catalogue.Entry{}, err
	}
	return catalogue.GetEntry(c.Param("product"))
}
