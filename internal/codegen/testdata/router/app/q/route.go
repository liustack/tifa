package query

import (
	"strings"

	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (string, error) {
	return c.Query("q") + c.Query("__proto__") + strings.Join(c.QueryValues("tag"), ","), nil
}
