package page

import "github.com/liustack/tifa"

type A struct{ ID int }

func Load(c *tifa.Ctx) (string, error) {
	err := func() error { return tifa.ClearSession(c) }()
	return "x", err
}

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &A{ID: 1}) }
