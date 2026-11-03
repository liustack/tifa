package blog

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/content"
)

type Item struct {
	content.Entry
	Title string `json:"title"`
}

var Items = content.Define[Item](content.Source{Dir: "x"})

func Load(c *tifa.Ctx) (string, error) { return "x", nil }
