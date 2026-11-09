package orders

import (
	"github.com/liustack/tifa"
)

func (in NewOrder) Validate() error { return CheckNewOrder(in) }

// POST places an order in the project and answers with it. Only the products and their quantities
// are sent: names, prices and stock come from the catalogue of the project, and the time is the
// time of the server.
func POST(c *tifa.Ctx, in NewOrder) (Order, error) {
	id, err := CreateOrder(c.Param("project"), in, Now())
	if err != nil {
		return Order{}, err
	}
	return GetOrder(c.Param("project"), id)
}
