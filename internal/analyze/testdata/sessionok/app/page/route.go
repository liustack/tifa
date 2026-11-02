package page

import "github.com/liustack/tifa"

type A struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
}

type Public struct {
	Name string `json:"name"`
}

func (a *A) Browser() Public { return Public{Name: a.Email} }

func Load(c *tifa.Ctx) (string, error) {
	if tifa.Session[A](c) == nil {
		return "out", nil
	}
	return "in", nil
}

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &A{ID: 1}) }

func DELETE(c *tifa.Ctx) error { return tifa.ClearSession(c) }
