package filters

import "github.com/liustack/tifa"

func Sort(c *tifa.Ctx) string {
	return c.Query("sort")
}
