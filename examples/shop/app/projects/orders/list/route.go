package list

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/orders"
)

const Path = "/projects/{projectId}"

func Load(c *tifa.Ctx) (orders.Summary, error) {
	return orders.List(c.Param("projectId")), nil
}
