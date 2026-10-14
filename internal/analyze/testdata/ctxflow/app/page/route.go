package page

import "github.com/liustack/tifa"

// The directory is the URL.

type querier interface{ Query(string) string }

type pair struct {
	C *tifa.Ctx
	V any
}

type box[T any] struct{ V T }

var global *tifa.Ctx

func takeAny(v any) string { return "" }

func generic[T any](v T) string { return "" }

func twice(c *tifa.Ctx) (*tifa.Ctx, error) { return nil, nil }

func Load(c *tifa.Ctx) (string, error) {
	var q querier
	q, _ = map[int]*tifa.Ctx{0: c}[0]
	a := any(c).(interface{ Query(string) string }).Query("tab")
	var i any = c
	n := len([]*tifa.Ctx{c})
	make(chan any, 1) <- c
	for range []*tifa.Ctx{} {
	}
	_ = pair{C: c, V: c}
	_ = box[*tifa.Ctx]{V: c}
	_ = &c
	x := c
	x = c
	global = c
	g := global.Query("g")
	defer x.Next()
	defer takeAny(c)
	got, _ := twice(c)
	ret := func() *tifa.Ctx { return c }
	s := takeAny(c) + generic(c) + generic[*tifa.Ctx](c)
	_, _, _ = i, n, ret
	return q.Query("x") + a + g + s + got.Query("ok"), nil
}
