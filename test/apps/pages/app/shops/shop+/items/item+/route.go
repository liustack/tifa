package item

import (
	"example.com/pagestest/app/shops/shop+"
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (*shop.Item, error) {
	item, ok := shop.FindItem(c.Param("item"))
	if !ok {
		return nil, tifa.ErrNotFound
	}
	return &item, nil
}

func POST(c *tifa.Ctx) error {
	if _, ok := shop.FindItem(c.Param("item")); !ok {
		return tifa.ErrNotFound
	}
	return c.NoContent(204)
}
