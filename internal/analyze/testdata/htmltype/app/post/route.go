package post

import "github.com/liustack/tifa"

type Meta struct {
	Summary tifa.HTML `json:"summary"`
}

// Post holds tifa.HTML in a field, through a pointer, in a slice, in a map value and in an
// embedded struct.
type Post struct {
	Meta
	Title  string               `json:"title"`
	Body   tifa.HTML            `json:"body"`
	Parts  []*tifa.HTML         `json:"parts"`
	ByLang map[string]tifa.HTML `json:"byLang"`
}

func Load(c *tifa.Ctx) (Post, error) { return Post{}, nil }
