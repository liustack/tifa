package project

import (
	"example.com/shop/app"
	"github.com/liustack/tifa"
)

const Memo = false

// Filter is the query string of the page.
type Filter struct {
	Sort string   `json:"sort"`
	Tag  []string `json:"tag"`
}

func Load(c *tifa.Ctx, q Filter) ([]string, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return nil, err
	}
	return append([]string{p.ID, q.Sort}, q.Tag...), nil
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
	return Project{ID: c.Param("projectId"), Name: site.Name}, nil
}
