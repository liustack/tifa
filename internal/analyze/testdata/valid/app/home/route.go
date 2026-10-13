package home

import "github.com/liustack/tifa"

const Path = "/"

func Load(c *tifa.Ctx) (string, error) { return c.Query("ref"), nil }
