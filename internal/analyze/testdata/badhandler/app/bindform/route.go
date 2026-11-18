package bindform

import "github.com/liustack/tifa"

type Filter struct {
	Tag string `form:"t" json:"tag"`
}

type Page struct {
	Filter Filter `json:"filter"`
	Size   int    `json:"size"`
}

func POST(c *tifa.Ctx) error {
	var in Page
	return c.Bind(&in)
}

func GET(c *tifa.Ctx) error {
	var q struct {
		Tag string `form:"t"`
	}
	return c.BindQuery(&q)
}
