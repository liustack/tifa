package app

import "github.com/liustack/tifa"

func Layout(c *tifa.Ctx) (string, error) { return c.Header("Accept-Language"), nil }
