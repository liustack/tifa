package page

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return "x", nil }

func POST(c *tifa.Ctx) error { n := 1; return tifa.SetSession(c, &n) }
