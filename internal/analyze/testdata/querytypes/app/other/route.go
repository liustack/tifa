package other

import "github.com/liustack/tifa"

type Filter struct {
	Page int `json:"page"`
}

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
