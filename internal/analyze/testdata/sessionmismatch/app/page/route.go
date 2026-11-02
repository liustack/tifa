package page

import "github.com/liustack/tifa"

type A struct{ ID int }

type B struct{ Name string }

func Load(c *tifa.Ctx) (string, error) {
	if tifa.Session[B](c) == nil {
		return "out", nil
	}
	return "in", nil
}

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &A{ID: 1}) }
