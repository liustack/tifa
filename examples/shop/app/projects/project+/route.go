// Package project is the project workspace: its Layout applies to /projects/{project} and every
// page below it, and its page lists the project's orders.
package project

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

func Load(c *tifa.Ctx) (orders.OrderList, error) {
	return orders.ListOrders(c.Param("project")), nil
}

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
