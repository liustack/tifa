package app

import "github.com/liustack/tifa"

func GET(c *tifa.Ctx) error {
	return c.Redirect("/projects/studio", 302)
}
