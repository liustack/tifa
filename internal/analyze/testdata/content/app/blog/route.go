package blog

import (
	"github.com/liustack/tifa"

	site "example.com/content"
)

type List struct {
	Posts []site.Post `json:"posts"`
}

func Load(c *tifa.Ctx) (List, error) { return List{Posts: site.Blog.In("en")}, nil }
