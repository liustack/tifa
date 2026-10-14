package shop

import (
	"github.com/liustack/tifa"
)

func Layout(c *tifa.Ctx) (Shop, error) {
	id := c.Param("shop")
	if id == "closed" {
		return Shop{}, tifa.ErrNotFound
	}
	return Shop{ID: id, Name: "Shop " + id}, nil
}
