package page

import (
	"github.com/liustack/tifa"

	"example.com/sessionload/internal/auth"
)

type A struct{ ID int }

func Load(c *tifa.Ctx) (string, error) {
	if err := tifa.SetSession(c, &A{ID: 1}); err != nil {
		return "", err
	}
	return "x", auth.Leave(c)
}

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &A{ID: 2}) }
