package alone

import (
	"example.com/replacebad/app/shop"
	"github.com/liustack/tifa"
)

func Layout(c *tifa.Ctx) (string, error) { return tifa.Use(c, shop.Layout) }
