package badsig

import "github.com/liustack/tifa"

type In struct {
	Name string `json:"name"`
}

func HEAD(c *tifa.Ctx) (string, error) { return "", nil }

func OPTIONS(c *tifa.Ctx, in In) error { return nil }

func CONNECT(c *tifa.Ctx) error { return nil }

func TRACE(c *tifa.Ctx) (In, error) { return In{}, nil }
