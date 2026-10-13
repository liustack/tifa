package page

import (
	"example.com/ctxallowed/db"
	"github.com/liustack/tifa"
)

const Path = "/page"

func Load(c *tifa.Ctx) (string, error) {
	x := c
	var y *tifa.Ctx = x
	var z = y
	r := db.Req{Ctx: z, Note: "plain"}
	d := db.Deep{R: r, Ptr: &db.Req{}}
	inline := func(k *tifa.Ctx) string { return k.Query("inline") }(c)
	later := func() string { return c.Query("closure") }
	if err := c.Next(); err != nil {
		return "", err
	}
	return r.Page() + r.Sort() + d.Size() + db.Read(c, r) + (c).Query("paren") + inline + later(), nil
}
