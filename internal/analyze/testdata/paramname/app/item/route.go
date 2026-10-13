package item

import "github.com/liustack/tifa"

const Path = "/items/{key}"

func Load(c *tifa.Ctx) (string, error) {
	name := "tab"
	return c.Param("id") + c.Query(name), nil
}
