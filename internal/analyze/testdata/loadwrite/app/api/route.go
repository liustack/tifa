package api

import "github.com/liustack/tifa"

// A method handler writes its own answer.
func POST(c *tifa.Ctx) error { return c.JSON(200, "ok") }
