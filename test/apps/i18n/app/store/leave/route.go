// Package leave is a page of the area app/store whose Load redirects to a page outside it.
package leave

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (struct{}, error) { return struct{}{}, tifa.Redirect("/projects", 302) }
