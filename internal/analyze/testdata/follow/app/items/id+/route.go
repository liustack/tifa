package list

import (
	"example.com/follow/app"
	"example.com/follow/auth"
	"example.com/follow/db"
	"github.com/liustack/tifa"
)

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) {
	site, err := shared(c)
	return site + db.Sort(c) + db.Walk(c, 3) + db.Req{Ctx: c}.Page() + auth.User(c), err
}

// shared uses the root Layout and the id parameter on behalf of Load.
func shared(c *tifa.Ctx) (string, error) {
	site, err := tifa.Use(c, app.Layout)
	return site + c.Param("id"), err
}
