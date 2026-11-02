package page

import "github.com/liustack/tifa"

type A struct{ ID int }

func Load(c *tifa.Ctx) (string, error) {
	if tifa.Session[A](c) == nil {
		return "out", nil
	}
	return "in", nil
}

func POST(c *tifa.Ctx) error { return tifa.ClearSession(c) }
