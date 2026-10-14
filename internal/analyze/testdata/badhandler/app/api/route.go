package api

import "github.com/liustack/tifa"

type Nested struct {
	Inner struct{ A string } `json:"inner"`
}

type Tagged struct {
	Name string `json:"name" form:"n"`
}

type BadValidate struct{ A string }

func (BadValidate) Validate() bool { return true }

type Embeds struct{ Tagged }

func GET(c *tifa.Ctx, in Nested) error { return nil }

func POST(c *tifa.Ctx, in *Tagged) error { return nil }

func PUT(c *tifa.Ctx, in Tagged) error { return nil }

func PATCH(c *tifa.Ctx, in BadValidate) error { return nil }

func DELETE(c *tifa.Ctx, in Embeds) error { return nil }
