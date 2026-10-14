package empty

import "github.com/liustack/tifa"

const Path = "/empty"

func Load(c *tifa.Ctx, extra int) (string, error) { return "", nil }

func POST(c *tifa.Ctx) error { return nil }
