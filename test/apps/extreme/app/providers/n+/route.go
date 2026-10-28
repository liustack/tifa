// Package n wraps the page in n providers, each of its own context, the way apps stack theme,
// session, flags and the providers of their libraries. The innermost component reads them all.
package n

import (
	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Stack struct {
	Count int `json:"count"`
}

func Load(c *tifa.Ctx) (Stack, error) {
	n, err := seed.Size(c.Param("n"), 1, 20_000)
	return Stack{Count: n}, err
}
