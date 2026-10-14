package items

import (
	"example.com/pagestest/app/shop"
	"github.com/liustack/tifa"
)

const Path = "/shops/{shopId}/items/{itemId}"

func Load(c *tifa.Ctx) (*shop.Item, error) {
	item, ok := shop.FindItem(c.Param("itemId"))
	if !ok {
		return nil, tifa.ErrNotFound
	}
	return &item, nil
}

func POST(c *tifa.Ctx) error {
	if _, ok := shop.FindItem(c.Param("itemId")); !ok {
		return tifa.ErrNotFound
	}
	return c.NoContent(204)
}
