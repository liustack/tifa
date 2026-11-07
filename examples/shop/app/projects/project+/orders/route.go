package orders

import (
	"time"

	"github.com/liustack/tifa"
)

func (in NewOrder) Validate() error { return CheckNewOrder(in) }

// POST places an order in the project and answers with it. Only the products and their quantities
// are sent: names, prices and stock come from the catalogue of the project, and the time is the
// time of the server.
func POST(c *tifa.Ctx, in NewOrder) (Order, error) {
	id, err := CreateOrder(c.Param("project"), in, time.Now())
	if err != nil {
		return Order{}, err
	}
	return GetOrder(c.Param("project"), id)
}

// PageInput is the page of the order list to read: 0 is the first, which the list page comes with.
type PageInput struct {
	Page int `json:"page"`
}

// GET answers with one page of the order list, newest first, for the pages after the first.
func GET(c *tifa.Ctx, in PageInput) ([]OrderRow, error) {
	return ListPage(c.Param("project"), in.Page)
}
