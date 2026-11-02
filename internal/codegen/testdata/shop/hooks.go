package shop

import (
	"context"

	"github.com/liustack/tifa"
)

func OnStart(ctx context.Context) error { return nil }

func OnError(c *tifa.Ctx, err error) tifa.Error { return tifa.Error{Status: 500, Message: "oops"} }
