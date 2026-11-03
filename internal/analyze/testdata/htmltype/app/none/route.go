package none

import "github.com/liustack/tifa"

// Plain holds no tifa.HTML that JSON writes: a dropped field and an array that is always [].
type Plain struct {
	Title string       `json:"title"`
	Gone  tifa.HTML    `json:"-"`
	Empty [0]tifa.HTML `json:"empty"`
}

func Load(c *tifa.Ctx) (Plain, error) { return Plain{}, nil }
