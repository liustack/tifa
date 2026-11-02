package page

import "github.com/liustack/tifa"

type A struct{ ID int }

func current[T any](c *tifa.Ctx) *T { return tifa.Session[T](c) }

func Load(c *tifa.Ctx) (string, error) {
	if current[A](c) == nil {
		return "out", nil
	}
	return "in", nil
}

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &A{ID: 1}) }
