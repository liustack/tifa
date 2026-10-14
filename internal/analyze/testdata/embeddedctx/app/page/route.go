package page

import "github.com/liustack/tifa"

// The directory is the URL.

type wrapped struct{ *tifa.Ctx }

func Load(c *tifa.Ctx) (string, error) {
	return wrapped{c}.Query("lang"), nil
}
