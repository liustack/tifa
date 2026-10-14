// Package home sends visitors of the site root to the first project.
package home

import "github.com/liustack/tifa"

const Path = "/"

func GET(c *tifa.Ctx) error {
	return c.Redirect("/projects/studio", 302)
}
