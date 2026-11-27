package api

import "github.com/liustack/tifa"

func OPTIONS(c *tifa.Ctx) error { return c.NoContent(204) }

func HEAD(c *tifa.Ctx) error { return c.NoContent(200) }

func GET(c *tifa.Ctx) (string, error) { return "ok", nil }

func TRACE(c *tifa.Ctx) error { return c.Text(200, "") }
