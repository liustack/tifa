package app

import "github.com/liustack/tifa"

type Paging struct {
	Page int `json:"page"`
	Sort string
}

func Layout(c *tifa.Ctx, q Paging) (string, error) { return "", nil }
