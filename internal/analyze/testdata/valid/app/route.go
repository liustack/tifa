package app

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return c.Query("ref"), nil }
