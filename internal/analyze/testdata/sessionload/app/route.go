package app

import "github.com/liustack/tifa"

func Layout(c *tifa.Ctx) (string, error) { return "layout", tifa.ClearSession(c) }
