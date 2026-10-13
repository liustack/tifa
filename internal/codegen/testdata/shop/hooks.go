package shop

import "github.com/liustack/tifa"

func OnStart() error { return nil }

func OnError(c *tifa.Ctx, err error) tifa.Error { return tifa.Error{Status: 500, Message: "oops"} }
