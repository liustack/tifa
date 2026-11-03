package lang

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return c.Param("lang"), nil }
