package login

import "github.com/liustack/tifa"

const Path = "/login"

const SSR = false

func POST(c *tifa.Ctx) error { return c.Redirect("/", 303) }
