package ping

import "github.com/liustack/tifa"

const Path = "/api/ping"

func OnGet(c *tifa.Ctx) error { return c.Text(200, "pong") }
