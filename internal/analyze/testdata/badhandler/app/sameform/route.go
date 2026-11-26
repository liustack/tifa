package sameform

import "github.com/liustack/tifa"

// A type that another framework reads with form tags too. A form tag that names the field as the
// json tag, or the Go name, does, is no error.
type Shared struct {
	Email string `json:"email" form:"email"`
	Name  string `form:"Name"`
	Opt   string `json:"opt,omitempty" form:"opt,omitempty"`
}

func GET(c *tifa.Ctx, q Shared) error { return nil }

func POST(c *tifa.Ctx, in Shared) error { return nil }

func PUT(c *tifa.Ctx) error {
	var in Shared
	return c.Bind(&in)
}

func PATCH(c *tifa.Ctx) error {
	var q Shared
	return c.BindQuery(&q)
}
