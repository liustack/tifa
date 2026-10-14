package item

import "github.com/liustack/tifa"

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) {
	name := "tab"
	return c.Param("id") + c.Query(name), nil
}
