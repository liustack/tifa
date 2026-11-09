// Package product is the edit page of a product in the back office: staff change its price, stock,
// title and description. In the studio shop the storefront shows the change on its next request.
package product

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
	catalogue "github.com/liustack/tifa/examples/shop/app/store/products"
)

// Load reads the product of the URL in the shop of the project.
func Load(c *tifa.Ctx) (catalogue.Entry, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return catalogue.Entry{}, err
	}
	return orders.ShopEntry(p.ID, c.Param("product"))
}

// PATCH changes the fields of the product that the body has and answers with the product. The edit
// page sends every field, and a field that is missing or null keeps its value. The change checks
// itself with Validate before it gets here.
func PATCH(c *tifa.Ctx, in catalogue.Change) (catalogue.Entry, error) {
	if err := orders.UpdateShopProduct(c.Param("project"), c.Param("product"), in); err != nil {
		return catalogue.Entry{}, err
	}
	return orders.ShopEntry(c.Param("project"), c.Param("product"))
}
