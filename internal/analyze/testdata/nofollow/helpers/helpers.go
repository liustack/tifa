package helpers

import (
	"example.com/nofollow/app/other"
	"github.com/liustack/tifa"
)

type querier interface{ Query(string) string }

type sink interface{ Take(*tifa.Ctx) }

var s sink

// Indirect hands the Ctx to an interface method.
func Indirect(c *tifa.Ctx) string {
	s.Take(c)
	return ""
}

// MethodValue hides the parameter name behind a method value.
func MethodValue(c *tifa.Ctx) string {
	f := c.Query
	return f("x")
}

// Iface converts the Ctx to an interface with Query.
func Iface(c *tifa.Ctx) string {
	var q querier = c
	return q.Query("y")
}

// Other uses a Layout that is not an ancestor of every page that reaches it.
func Other(c *tifa.Ctx) string {
	s, _ := tifa.Use(c, other.Load)
	return s
}
