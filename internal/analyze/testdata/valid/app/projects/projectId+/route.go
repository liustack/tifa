package project

import (
	"strconv"

	"example.com/valid/app"
	"github.com/liustack/tifa"
)

const (
	SSR  = false
	Memo = false
)

// Filter is the query string of the project page.
type Filter struct {
	Sort string `json:"sort"`
	Page int    `json:"page"`
}

// The page in the Layout's own directory uses the Layout function of its own package.
func Load(c *tifa.Ctx, q Filter) ([]string, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return nil, err
	}
	return []string{p.ID, q.Sort, strconv.Itoa(q.Page)}, nil
}

func Layout(c *tifa.Ctx) (Project, error) {
	site, err := tifa.Use(c, app.Layout)
	if err != nil {
		return Project{}, err
	}
	return Project{ID: c.Param("projectId"), Name: site.Name}, nil
}
