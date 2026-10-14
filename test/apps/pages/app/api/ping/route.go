package ping

import "github.com/liustack/tifa"

const Path = "/api/ping"

func GET(c *tifa.Ctx) error { return c.Text(200, "pong") }
