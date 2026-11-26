package path

import "github.com/liustack/tifa"

type Out struct{}

func GET(c *tifa.Ctx) (Out, error) { return Out{}, nil }
