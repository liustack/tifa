package page

import "github.com/liustack/tifa"

type A struct{ ID int }

type Public struct {
	Name string
	Ch   chan int
}

func (a *A) Exposed() Public { return Public{} }

func Load(c *tifa.Ctx) (string, error) { return "x", nil }

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &A{ID: 1}) }
