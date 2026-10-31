package account

import (
	"example.com/replace/app/store/lang+"
	"github.com/liustack/tifa"
)

// The Layout of the folder with layout_.tsx still wraps this one.
func Layout(c *tifa.Ctx) (string, error) { return tifa.Use(c, lang.Layout) }
