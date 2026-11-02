package valid

import (
	"context"

	"github.com/liustack/tifa"
)

func OnStart(ctx context.Context) error { return nil }

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error { return next(c) }
