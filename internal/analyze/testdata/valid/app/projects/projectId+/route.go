package project

import "github.com/liustack/tifa"

const Cache = true

// The page in the Layout's own directory uses the Layout function of its own package.
func Load(c *tifa.Ctx) ([]string, error) {
	p, err := tifa.Use(c, Layout)
	if err != nil {
		return nil, err
	}
	return []string{p.ID, c.Query("sort"), c.Query("page"), c.Query("page")}, nil
}
