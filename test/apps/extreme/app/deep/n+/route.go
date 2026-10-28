// Package n nests n divs in each other. Browsers stop nesting at about 512 levels when they
// parse HTML, so the deeper sizes only render on the server as they should.
package n

import (
	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Deep struct {
	Depth int `json:"depth"`
}

func Load(c *tifa.Ctx) (Deep, error) {
	n, err := seed.Size(c.Param("n"), 1, 100_000)
	return Deep{Depth: n}, err
}
