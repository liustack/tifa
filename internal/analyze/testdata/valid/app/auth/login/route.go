package login

import "github.com/liustack/tifa"

const Path = "/login"

func OnPost(c *tifa.Ctx) error { return c.NoContent(204) }
