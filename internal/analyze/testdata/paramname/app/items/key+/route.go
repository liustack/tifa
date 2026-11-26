package item

import (
	"github.com/liustack/tifa"
)

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) {
	name := "key"
	return c.Param("id") + c.Param(name), nil
}

func POST(c *tifa.Ctx) error {
	_ = c.Param("key")
	return readOther(c)
}

func readOther(c *tifa.Ctx) error {
	_ = c.Param("other")
	return c.NoContent(204)
}
