package helper

import "github.com/liustack/tifa"

type Form struct{ A string }

// Validate hands the Ctx to a helper, which the analyzer follows.
func (Form) Validate(c *tifa.Ctx) error { return check(c) }

func check(c *tifa.Ctx) error {
	c.SetCookie(nil)
	return nil
}

var saved *tifa.Ctx

type Keeps struct{ A string }

func (Keeps) Validate(c *tifa.Ctx) error {
	saved = c
	return nil
}

func POST(c *tifa.Ctx, in Form) error { return nil }

func PUT(c *tifa.Ctx, in Keeps) error { return nil }
