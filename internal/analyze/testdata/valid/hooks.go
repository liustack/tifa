package valid

import "github.com/liustack/tifa"

func OnStart() error { return nil }

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error { return next(c) }
