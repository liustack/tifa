package project

import (
	"example.com/shop/app"
	"github.com/liustack/tifa"
)

const Cache = true

func Load(c *tifa.Ctx) ([]string, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return nil, err
	}
	return []string{p.ID, c.Query("sort")}, nil
}

type Project struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Parent *Project `json:"parent"`
}

func Layout(c *tifa.Ctx) (Project, error) {
	site, err := tifa.Use(c, app.Layout)
	if err != nil {
		return Project{}, err
	}
	return Project{ID: c.Param("projectId"), Name: site.Name + c.Query("currency")}, nil
}
