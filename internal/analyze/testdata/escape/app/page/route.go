package page

import (
	"example.com/escape/db"
	"github.com/liustack/tifa"
)

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) {
	tab := c.Request().URL.Query().Get("tab")
	(*tifa.Ctx).Writer(c).Header().Set("X", "1")
	db.Req{Ctx: c}.Flush()
	return tab + db.Agent(c), nil
}

// POST may use the raw request and writer.
func POST(c *tifa.Ctx) error {
	c.Writer().Header().Set("X", c.Request().Method)
	return nil
}
