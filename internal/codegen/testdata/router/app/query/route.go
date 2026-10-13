package query

import "github.com/liustack/tifa"

const Path = "/q"

func Load(c *tifa.Ctx) (string, error) { return c.Query("q"), nil }
