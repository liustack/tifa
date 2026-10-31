package desk

import "github.com/liustack/tifa"

func Layout(c *tifa.Ctx) (string, error) { return "desk", nil }

func GET(c *tifa.Ctx) error { return c.Text(200, "desk") }
