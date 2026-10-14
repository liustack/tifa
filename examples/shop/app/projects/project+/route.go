package project

import (
	"github.com/liustack/tifa"
	shop "github.com/liustack/tifa/examples/shop"
)

func Load(c *tifa.Ctx) (shop.OrderList, error) {
	return shop.ListOrders(c.Param("project")), nil
}
