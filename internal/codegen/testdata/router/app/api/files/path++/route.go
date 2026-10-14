package path

import "github.com/liustack/tifa"

func GET(c *tifa.Ctx) error { return c.Text(200, c.Param("path")) }
