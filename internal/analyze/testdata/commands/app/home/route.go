package home

import "github.com/liustack/tifa"

func GET(c *tifa.Ctx) error { return c.NoContent(204) }
