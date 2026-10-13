package login

import "github.com/liustack/tifa"

const Path = "/login"

const SSR = false

func OnPost(c *tifa.Ctx) error { return c.NoContent(204) }
