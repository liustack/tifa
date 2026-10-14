package path

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return c.Param("path"), nil }
