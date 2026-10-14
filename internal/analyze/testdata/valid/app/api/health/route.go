package health

import "github.com/liustack/tifa"

const Path = "/api/health"

func GET(c *tifa.Ctx) error { return c.Text(200, "ok") }
