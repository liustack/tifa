package probe

import "github.com/liustack/tifa"

// A route with HEAD alone.
func HEAD(c *tifa.Ctx) error { return c.NoContent(204) }
