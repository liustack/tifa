// Package order is the order detail page.
package order

import (
	"github.com/liustack/tifa"
	shop "github.com/liustack/tifa/examples/shop"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
)

// Detail pages change only when an order ships, so their HTML is cached.
const Cache = true

// Load reads the project through its Layout, which loads it once for the whole request.
func Load(c *tifa.Ctx) (shop.Order, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return shop.Order{}, err
	}
	return shop.GetOrder(p.ID, c.Param("order"))
}

// POST ships the order and answers with it.
func POST(c *tifa.Ctx) (shop.Order, error) {
	if err := shop.ShipOrder(c.Param("project"), c.Param("order")); err != nil {
		return shop.Order{}, err
	}
	return shop.GetOrder(c.Param("project"), c.Param("order"))
}
