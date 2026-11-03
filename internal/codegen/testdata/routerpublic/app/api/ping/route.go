package ping

import "github.com/liustack/tifa"

func GET(c *tifa.Ctx) (string, error) { return "pong", nil }
