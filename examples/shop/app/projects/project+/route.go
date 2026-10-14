package project

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

func Load(c *tifa.Ctx) (orders.OrderList, error) {
	return orders.ListOrders(c.Param("project")), nil
}
