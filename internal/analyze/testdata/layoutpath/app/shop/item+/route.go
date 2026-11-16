package item

import "github.com/liustack/tifa"

// A page's own Load may read the path, and so may a helper it calls.
func Load(c *tifa.Ctx) (string, error) {
	return c.Path() + here(c), nil
}

func here(c *tifa.Ctx) string { return c.Path() }
