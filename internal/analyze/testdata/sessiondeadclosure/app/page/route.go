package page

import "github.com/liustack/tifa"

type A struct{ ID int }

// The closure is declared and never run, so Load does not sign anyone out.
func Load(c *tifa.Ctx) (string, error) {
	_ = func() error { return tifa.ClearSession(c) }
	return "x", nil
}

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &A{ID: 1}) }
