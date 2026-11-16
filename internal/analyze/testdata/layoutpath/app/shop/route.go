package shop

import "github.com/liustack/tifa"

// The data of a Layout is shared by every page below it, so it cannot hold the path of one of them.
func Layout(c *tifa.Ctx) (string, error) {
	return c.Path() + where(c), nil
}

func where(c *tifa.Ctx) string { return c.Path() }
