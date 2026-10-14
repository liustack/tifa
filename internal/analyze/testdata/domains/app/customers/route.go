// Package customers is the customer domain and the customer list page.
package customers

import (
	"github.com/liustack/tifa"

	"example.com/domains/app/projects/project+/orders"
)

type Customer struct {
	Name   string `json:"name"`
	Orders int    `json:"orders"`
}

func Load(c *tifa.Ctx) ([]Customer, error) {
	return []Customer{{Name: "a", Orders: len(orders.ListOrders("a"))}}, nil
}
