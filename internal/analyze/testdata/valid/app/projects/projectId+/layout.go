package project

import (
	"example.com/valid/app"
	"github.com/liustack/tifa"
)

const LayoutSSR = false

func Layout(c *tifa.Ctx) (Project, error) {
	site, err := tifa.Use(c, app.Layout)
	if err != nil {
		return Project{}, err
	}
	return Project{ID: c.Param("projectId"), Name: site.Name + c.Query("currency")}, nil
}
