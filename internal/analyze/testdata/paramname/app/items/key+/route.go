package item

import (
	"github.com/liustack/tifa"
)

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) {
	name := "key"
	return c.Param("id") + c.Param(name), nil
}
