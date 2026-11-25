package filter

import "github.com/liustack/tifa"

type Filter struct {
	Page int `json:"page"`
}

// Read decodes the query string through a helper.
func Read(c *tifa.Ctx) (Filter, error) {
	var f Filter
	err := c.BindQuery(&f)
	return f, err
}
