package orders

import (
	"errors"

	"github.com/liustack/tifa"
)

const Path = "/orders/{orderId}"

type Order struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

func GET(c *tifa.Ctx) error {
	switch id := c.Param("orderId"); id {
	case "missing":
		return tifa.ErrNotFound
	case "shipped":
		return tifa.Error{Status: 409, Message: "order already shipped"}
	case "broken":
		return errors.New("database down")
	default:
		return c.JSON(200, Order{ID: id})
	}
}

func POST(c *tifa.Ctx) error {
	var in Order
	if err := c.Bind(&in); err != nil {
		return err
	}
	in.ID = c.Param("orderId")
	return c.JSON(201, in)
}
