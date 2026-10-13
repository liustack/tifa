package list

import (
	"example.com/valid/app/projects"
	"github.com/liustack/tifa"
)

const Path = "/projects/{projectId}"

const Cache = true

func Load(c *tifa.Ctx) ([]string, error) {
	p, err := tifa.Use(c, projects.Load)
	if err != nil {
		return nil, err
	}
	return []string{p.ID, c.Query("sort"), c.Query("page"), c.Query("page")}, nil
}
