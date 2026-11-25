package page

import (
	"example.com/uselayoutquery/app"
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (string, error) {
	return tifa.Use(c, app.Layout)
}
