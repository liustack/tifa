package auth

import "github.com/liustack/tifa"

func Leave(c *tifa.Ctx) error { return tifa.ClearSession(c) }
