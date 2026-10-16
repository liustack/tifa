// Package stats is the sales stats page of a project.
package stats

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

// Load reads the project through its Layout first, so an unknown project is a 404.
func Load(c *tifa.Ctx) (orders.Sales, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return orders.Sales{}, err
	}
	return orders.SalesStats(p.ID), nil
}
