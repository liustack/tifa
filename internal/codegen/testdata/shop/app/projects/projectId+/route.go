package project

import "github.com/liustack/tifa"

const Cache = true

func Load(c *tifa.Ctx) ([]string, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return nil, err
	}
	return []string{p.ID, c.Query("sort")}, nil
}
