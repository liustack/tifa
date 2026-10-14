package page

import (
	"example.com/indirect/app"
	"github.com/liustack/tifa"
)

const Path = "/page"

type useFunc func(*tifa.Ctx, func(*tifa.Ctx) (string, error)) (string, error)

func pass(u useFunc, c *tifa.Ctx) {}

type wrapped struct{ *tifa.Ctx }

func Load(c *tifa.Ctx) (string, error) {
	q := c.Query
	tab := (*tifa.Ctx).Query(c, "tab")
	use := tifa.Use[string]
	pass(tifa.Use[string], c)
	s, err := use(c, app.Load)
	lang := wrapped{c}.Query("lang")
	return q("x") + tab + s + lang, err
}

func POST(c *tifa.Ctx) error {
	param := c.Param
	_ = param
	return nil
}
