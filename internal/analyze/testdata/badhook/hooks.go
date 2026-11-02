package badhook

import "github.com/liustack/tifa"

// The Ctx.Next form is gone: next is a parameter.
func OnRequest(c *tifa.Ctx) error { return nil }
