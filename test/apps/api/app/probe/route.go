// Package probe answers HEAD alone, and TRACE.
package probe

import "github.com/liustack/tifa"

func HEAD(c *tifa.Ctx) error {
	c.SetHeader("X-Probe", "up")
	return c.NoContent(204)
}

func TRACE(c *tifa.Ctx) error { return c.Text(200, "TRACE "+c.Path()) }
