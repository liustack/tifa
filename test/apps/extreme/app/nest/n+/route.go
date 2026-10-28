// Package n nests components that only pass their children on, n levels deep, with a div every
// 50 levels so the DOM stays under 400 levels.
package n

import (
	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Nest struct {
	Depth int `json:"depth"`
}

func Load(c *tifa.Ctx) (Nest, error) {
	n, err := seed.Size(c.Param("n"), 1, 100_000)
	return Nest{Depth: n}, err
}
