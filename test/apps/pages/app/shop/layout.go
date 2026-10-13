package shop

import (
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (Shop, error) {
	id := c.Param("shopId")
	if id == "closed" {
		return Shop{}, tifa.ErrNotFound
	}
	return Shop{ID: id, Name: "Shop " + id}, nil
}
