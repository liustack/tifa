package project

import (
	"github.com/liustack/tifa"

	"example.com/domains/app/projects"
)

func Layout(c *tifa.Ctx) (projects.Project, error) { return projects.GetProject(c.Param("project")) }
