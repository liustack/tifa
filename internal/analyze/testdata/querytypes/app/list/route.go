package list

import "github.com/liustack/tifa"

type Paging struct {
	Page int `json:"page"`
}

type Filter struct {
	Page string `json:"page"`
	Sort string
}

func Layout(c *tifa.Ctx, q Paging) (string, error) { return "", nil }

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
