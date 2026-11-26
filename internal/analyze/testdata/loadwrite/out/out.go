package out

import "github.com/liustack/tifa"

func Write(c *tifa.Ctx) error { return c.Text(200, "text") }
