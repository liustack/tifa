package order

import (
	"github.com/liustack/tifa"

	"example.com/domains/app/projects/project+"
	"example.com/domains/app/projects/project+/orders"
)

func Load(c *tifa.Ctx) (orders.Order, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return orders.Order{}, err
	}
	return orders.GetOrder(p.ID, c.Param("order"))
}
