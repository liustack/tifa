package home

import "github.com/liustack/tifa"

const Path = "/"

func OnGet(c *tifa.Ctx) error { return c.Text(200, "home") }
