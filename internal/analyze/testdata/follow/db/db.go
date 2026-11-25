package db

import "github.com/liustack/tifa"

// Sort reads the id parameter through two levels of helpers.
func Sort(c *tifa.Ctx) string { return order(c) }

func order(c *tifa.Ctx) string { return c.Param("id") + dir(c) }

func dir(c *tifa.Ctx) string { return c.Param("id") }

// Walk is recursive.
func Walk(c *tifa.Ctx, n int) string {
	if n == 0 {
		return c.Param("id")
	}
	return Walk(c, n-1)
}

// Req carries the Ctx by embedding.
type Req struct{ *tifa.Ctx }

func (r Req) Page() string { return r.Param("id") }
