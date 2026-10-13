package projects

import (
	"example.com/shop/app"
	"github.com/liustack/tifa"
)

type Project struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Parent *Project `json:"parent"`
}

func Load(c *tifa.Ctx) (Project, error) {
	site, err := tifa.Use(c, app.Load)
	if err != nil {
		return Project{}, err
	}
	return Project{ID: c.Param("projectId"), Name: site.Name + c.Query("currency")}, nil
}
