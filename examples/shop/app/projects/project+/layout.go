// Package project is the project workspace: its Layout applies to /projects/{project} and every
// page below it, and its page lists the project's orders.
package project

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects"
)

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
