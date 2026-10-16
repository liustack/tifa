package list

import (
	"example.com/bindquery/filter"
	"github.com/liustack/tifa"
)

// The directory is the URL.

func Load(c *tifa.Ctx) (int, error) {
	var f filter.Filter
	if err := c.BindQuery(&f); err != nil {
		return 0, err
	}
	g, err := filter.Read(c)
	if err != nil {
		return 0, err
	}
	var body struct{ Note string }
	if err := c.Bind(&body); err != nil {
		return 0, err
	}
	return f.Page + g.Page, nil
}

// POST may decode its input itself.
func POST(c *tifa.Ctx) error {
	var f filter.Filter
	if err := c.BindQuery(&f); err != nil {
		return err
	}
	return c.Bind(&f)
}
