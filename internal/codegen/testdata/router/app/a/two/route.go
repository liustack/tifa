package two

import "github.com/liustack/tifa"

const Path = "/shared/two"

func Load(c *tifa.Ctx) (string, error) { return "two", nil }
