package query

import "github.com/liustack/tifa"

const Path = "/q"

func Load(c *tifa.Ctx) (string, error) { return c.Query("q") + c.Query("__proto__"), nil }
