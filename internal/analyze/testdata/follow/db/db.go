package db

import "github.com/liustack/tifa"

// Sort reads two query parameters through two levels of helpers.
func Sort(c *tifa.Ctx) string { return order(c) }

func order(c *tifa.Ctx) string { return c.Query("sort") + dir(c) }

func dir(c *tifa.Ctx) string { return c.Query("dir") }

// Walk is recursive.
func Walk(c *tifa.Ctx, n int) string {
	if n == 0 {
		return c.Query("depth")
	}
	return Walk(c, n-1)
}

// Req carries the Ctx by embedding.
type Req struct{ *tifa.Ctx }

func (r Req) Page() string { return r.Query("page") }
