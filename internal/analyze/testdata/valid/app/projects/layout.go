package projects

import (
	"example.com/valid/app"
	"github.com/liustack/tifa"
)

const SSR = false

func Load(c *tifa.Ctx) (Project, error) {
	site, err := tifa.Use(c, app.Load)
	if err != nil {
		return Project{}, err
	}
	return Project{ID: c.Param("projectId"), Name: site.Name + c.Query("currency")}, nil
}
