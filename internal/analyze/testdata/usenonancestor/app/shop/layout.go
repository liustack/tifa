package shop

import "github.com/liustack/tifa"

func Layout(c *tifa.Ctx) (string, error) {
	return name(c)
}

func name(c *tifa.Ctx) (string, error) {
	return tifa.Use(c, Layout)
}
