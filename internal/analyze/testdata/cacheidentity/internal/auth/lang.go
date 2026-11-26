package auth

import "github.com/liustack/tifa"

// Lang runs for a page with Cache and for one without.
func Lang(c *tifa.Ctx) string { return c.Header("X-Lang") }
