package health

import "github.com/liustack/tifa"

const Path = "/health"

func GET(c *tifa.Ctx) error { return c.JSON(200, map[string]string{"status": "ok"}) }
