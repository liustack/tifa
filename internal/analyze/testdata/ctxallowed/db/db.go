package db

import "github.com/liustack/tifa"

// Req carries the Ctx by embedding, Deep carries it through Req.
type Req struct {
	*tifa.Ctx
	Note any
}

type Deep struct {
	R   Req
	Ptr *Req
}

func (r Req) Page() string { return r.Query("page") }

func (r *Req) Sort() string { return r.Query("sort") }

func (d Deep) Size() string { return d.R.Query("size") + d.Ptr.Sort() }

func Read(c *tifa.Ctx, r Req) string { return c.Query("q") + r.Page() }
