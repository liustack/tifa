package detail

import (
	"github.com/liustack/tifa"
	shop "github.com/liustack/tifa/examples/shop"
)

const Path = "/projects/{projectId}/orders/{orderId}"

// Detail pages change only when an order ships, so their HTML is cached.
const Cache = true

func Load(c *tifa.Ctx) (shop.Order, error) {
	return shop.GetOrder(c.Param("projectId"), c.Param("orderId"))
}

// POST ships the order.
func POST(c *tifa.Ctx) error {
	if err := shop.ShipOrder(c.Param("projectId"), c.Param("orderId")); err != nil {
		return err
	}
	return c.NoContent(204)
}
