// Package project is the project workspace: its Layout applies to /projects/{project} and every
// page below it, and its page lists the project's orders.
package project

import (
	"github.com/liustack/tifa"
	shop "github.com/liustack/tifa/examples/shop"
)

func Layout(c *tifa.Ctx) (shop.Project, error) {
	return shop.GetProject(c.Param("project"))
}
