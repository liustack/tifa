// Package project is the project workspace: its Layout applies to /projects/{project} and every
// page below it, and its page lists the project's orders.
package project

import (
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

// Load returns a page of the orders that the query string keeps, with numbers over all of them.
// The filters are status, carrier, period, amount and q (a search in order numbers and customers),
// sort (placedAt or totalCents) and dir (asc or desc), and page counts from 1: the fields of
// orders.ListQuery. A filter or a sort that is not one of the choices is left out, as if it were
// not there, and a page that is not a whole number answers 400. The days of its cards start at
// midnight in the time zone of the shop.
func Load(c *tifa.Ctx, q orders.ListQuery) (orders.OrderList, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return orders.OrderList{}, err
	}
	zone, err := time.LoadLocation(p.TimeZone)
	if err != nil {
		return orders.OrderList{}, err
	}
	return orders.ListOrders(p.ID, zone, q), nil
}

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
