package item

import (
	"strings"

	"github.com/liustack/tifa"
)

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) {
	name := "tab"
	return c.Param("id") + c.Query(name) + strings.Join(c.QueryAll(name), ","), nil
}
