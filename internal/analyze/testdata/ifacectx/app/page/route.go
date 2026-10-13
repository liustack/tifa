package page

import (
	"context"

	"github.com/liustack/tifa"
)

const Path = "/page/{id}"

type querier interface{ Query(string) string }

type paramer interface{ Param(name string) string }

func read(q querier) string { return q.Query("hidden") }

func Load(c *tifa.Ctx) (string, error) {
	a := interface{ Query(string) string }(c).Query("tab")
	var q querier = c
	b := read(c)
	f := func() paramer { return c }
	list := []querier{c}
	var ok1 any = c
	ok2 := interface{ Context() context.Context }(c)
	_, _, _ = q, list, ok1
	_ = ok2
	return a + b + f().Param("id"), nil
}
