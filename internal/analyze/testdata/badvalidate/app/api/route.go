package api

import "github.com/liustack/tifa"

type NoResult struct{ A string }

func (NoResult) Validate() bool { return true }

type WrongParam struct{ A string }

func (WrongParam) Validate(n int) error { return nil }

type TwoParams struct{ A string }

func (TwoParams) Validate(c *tifa.Ctx, n int) error { return nil }

type Writes struct{ A string }

func (Writes) Validate(c *tifa.Ctx) error {
	c.SetHeader("X-A", "b")
	return c.JSON(200, "no")
}

type Reads struct{ A string }

func (Reads) Validate(c *tifa.Ctx) error {
	var in Reads
	if err := c.Bind(&in); err != nil {
		return err
	}
	_ = c.Writer()
	return nil
}

func POST(c *tifa.Ctx, in NoResult) error { return nil }

func PUT(c *tifa.Ctx, in WrongParam) error { return nil }

func PATCH(c *tifa.Ctx, in TwoParams) error { return nil }

func DELETE(c *tifa.Ctx, in Writes) error { return nil }

func GET(c *tifa.Ctx, in Reads) error { return nil }
