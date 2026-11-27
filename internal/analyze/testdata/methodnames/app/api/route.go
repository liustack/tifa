package api

import "github.com/liustack/tifa"

type In struct {
	Name string `json:"name"`
}

func GET(c *tifa.Ctx) (string, error) { return "ok", nil }

func TRACE(c *tifa.Ctx) error { return nil }

func Connect(c *tifa.Ctx) error { return nil }

func Post(c *tifa.Ctx, in In) error { return nil }

func patch(c *tifa.Ctx, in In) error { return nil }

func Head(c *tifa.Ctx) error { return nil }

// Options is called from another package, so it is a helper.
func Options(c *tifa.Ctx) error { return nil }
