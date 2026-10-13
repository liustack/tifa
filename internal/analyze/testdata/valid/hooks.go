package valid

import "github.com/liustack/tifa"

func OnStart() error { return nil }

func OnRequest(c *tifa.Ctx) error { return c.Next() }
