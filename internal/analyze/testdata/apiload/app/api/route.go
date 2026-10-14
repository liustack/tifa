package api

import "github.com/liustack/tifa"

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) { return "", nil }

func GET(c *tifa.Ctx) error { return c.Text(200, "ok") }
