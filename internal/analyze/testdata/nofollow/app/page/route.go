package page

import (
	"fmt"

	"example.com/nofollow/helpers"
	"github.com/liustack/tifa"
)

const Path = "/page"

type reader interface{ Read(c *tifa.Ctx) string }

type impl struct{}

func (impl) Read(c *tifa.Ctx) string { return "" }

var fn = func(c *tifa.Ctx) string { return "" }

func background(c *tifa.Ctx) {}

func Load(c *tifa.Ctx) (string, error) {
	var r reader = impl{}
	a := r.Read(c)
	b := fn(c)
	go background(c)
	fmt.Println(c)
	go func() { _ = c.Query("z") }()
	return a + b + helpers.Indirect(c) + helpers.MethodValue(c) + helpers.Iface(c) + helpers.Other(c), nil
}
