package three

import "github.com/liustack/tifa"

type Filter struct{ Page int }

func Load(c *tifa.Ctx, q Filter, more int) (string, error) { return "", nil }

func Layout(c *tifa.Ctx, q Filter, more int) (string, error) { return "", nil }
