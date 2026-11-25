package embedded

import "github.com/liustack/tifa"

type Paging struct {
	Page int `json:"page"`
}

type Filter struct {
	Paging
	Sort string `json:"sort"`
}

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
