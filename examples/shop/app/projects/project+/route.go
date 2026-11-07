// Package project is the project workspace: its Layout applies to /projects/{project} and every
// page below it, and its page lists the project's orders.
package project

import (
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

// Load returns the first page of the orders, with numbers over all of them. The days of its cards
// start at midnight in the time zone of the shop.
func Load(c *tifa.Ctx) (orders.OrderList, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return orders.OrderList{}, err
	}
	zone, err := time.LoadLocation(p.TimeZone)
	if err != nil {
		return orders.OrderList{}, err
	}
	return orders.ListOrders(p.ID, zone), nil
}

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
