package home

import "github.com/liustack/tifa"

const Path = "/"

func GET(c *tifa.Ctx) error { return c.Text(200, "home") }
