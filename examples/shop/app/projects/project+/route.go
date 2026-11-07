// Package project is the project workspace: its Layout applies to /projects/{project} and every
// page below it, and its page lists the project's orders.
package project

import (
	"strconv"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

// Load returns a page of the orders that the query string keeps, with numbers over all of them.
// The filters are status, carrier, period, amount and q (a search in order numbers and customers),
// and page counts from 1. A filter that is not one of the choices is left out, as if it were not
// there. The days of its cards start at midnight in the time zone of the shop.
func Load(c *tifa.Ctx) (orders.OrderList, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return orders.OrderList{}, err
	}
	zone, err := time.LoadLocation(p.TimeZone)
	if err != nil {
		return orders.OrderList{}, err
	}
	page, _ := strconv.Atoi(c.Query("page"))
	return orders.ListOrders(p.ID, zone, orders.ListQuery{
		Status:  c.Query("status"),
		Carrier: c.Query("carrier"),
		Period:  c.Query("period"),
		Amount:  c.Query("amount"),
		Search:  c.Query("q"),
		Page:    page,
	}), nil
}

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
