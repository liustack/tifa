package list

import (
	"github.com/liustack/tifa"
	shop "github.com/liustack/tifa/examples/shop"
)

const Path = "/projects/{projectId}"

func Load(c *tifa.Ctx) (shop.OrderList, error) {
	return shop.ListOrders(c.Param("projectId")), nil
}
