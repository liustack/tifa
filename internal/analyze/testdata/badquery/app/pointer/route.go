package pointer

import "github.com/liustack/tifa"

type Filter struct{ Page int }

func Load(c *tifa.Ctx, q *Filter) (string, error) { return "", nil }
