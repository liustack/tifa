package project

import (
	"example.com/valid/app"
	"github.com/liustack/tifa"
)

const RenderCache = false

// The page in the Layout's own directory uses the Layout function of its own package.
func Load(c *tifa.Ctx) ([]string, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return nil, err
	}
	return []string{p.ID, c.Query("sort"), c.Query("page"), c.Query("page")}, nil
}

const LayoutSSR = false

func Layout(c *tifa.Ctx) (Project, error) {
	site, err := tifa.Use(c, app.Layout)
	if err != nil {
		return Project{}, err
	}
	return Project{ID: c.Param("projectId"), Name: site.Name + c.Query("currency")}, nil
}
