package page

import "github.com/liustack/tifa"

const Path = "/page"

type wrapped struct{ *tifa.Ctx }

func Load(c *tifa.Ctx) (string, error) {
	return wrapped{c}.Query("lang"), nil
}
