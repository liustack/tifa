package article

import "github.com/liustack/tifa"

type Post struct {
	Title string      `json:"title"`
	Body  tifa.HTML   `json:"body"`
	Notes []tifa.HTML `json:"notes"`
}

func Load(c *tifa.Ctx) (Post, error) { return Post{}, nil }
