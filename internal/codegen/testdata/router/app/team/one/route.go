package one

import "github.com/liustack/tifa"

const Path = "/teams/{team}/one"

func Load(c *tifa.Ctx) (string, error) { return "one", nil }
