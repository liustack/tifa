package ping

import "github.com/liustack/tifa"

func GET(c *tifa.Ctx) error { return c.Text(200, "pong") }
