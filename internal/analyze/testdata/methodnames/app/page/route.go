package page

import (
	"github.com/liustack/tifa"

	"example.com/methodnames/app/api"
)

// Post is the app's own type, and get a helper that Load calls: neither is reported.
type Post struct {
	Title string `json:"title"`
}

func get(c *tifa.Ctx) (Post, error) { return Post{Title: c.Path()}, nil }

func Load(c *tifa.Ctx) (Post, error) {
	if err := api.Options(c); err != nil {
		return Post{}, err
	}
	return get(c)
}

// Trace takes no Ctx, so it is the app's own function too.
func Trace(s string) string { return s }

func Delete(c *tifa.Ctx) error { return nil }

func HEAD(c *tifa.Ctx) error { return nil }

func OPTIONS(c *tifa.Ctx) error { return nil }
