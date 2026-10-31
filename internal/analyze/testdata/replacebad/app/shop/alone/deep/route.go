package deep

import (
	"example.com/replacebad/app"
	"example.com/replacebad/app/shop/alone"
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (string, error) {
	if _, err := tifa.Use(c, alone.Layout); err != nil { // the Layout of layout_.tsx still wraps this page
		return "", err
	}
	return tifa.Use(c, app.Layout)
}
