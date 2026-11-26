package app

import "github.com/liustack/tifa"

// The root Layout runs for pages with Cache and for pages without it, so its reads are left alone.
func Layout(c *tifa.Ctx) (string, error) { return c.Header("Accept-Language"), nil }
