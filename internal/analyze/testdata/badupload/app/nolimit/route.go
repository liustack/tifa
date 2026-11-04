package nolimit

import "github.com/liustack/tifa"

const BodyLimit = 1 << 20

func Layout(c *tifa.Ctx) (string, error) { return "", nil }
