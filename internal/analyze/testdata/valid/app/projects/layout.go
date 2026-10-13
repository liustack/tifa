package projects

import "github.com/liustack/tifa"

const SSR = false

func Load(c *tifa.Ctx) (Project, error) {
	return Project{ID: c.Param("projectId")}, nil
}
