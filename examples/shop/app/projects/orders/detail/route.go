package detail

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/orders"
)

const Path = "/projects/{projectId}/orders/{orderId}"

// Detail pages change only when an order ships, so their HTML is cached.
const Cache = true

func Load(c *tifa.Ctx) (orders.Order, error) {
	return orders.Get(c.Param("projectId"), c.Param("orderId"))
}

// OnPost ships the order.
func OnPost(c *tifa.Ctx) error {
	if err := orders.Ship(c.Param("projectId"), c.Param("orderId")); err != nil {
		return err
	}
	return c.NoContent(204)
}
