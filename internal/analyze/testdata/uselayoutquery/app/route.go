package app

import "github.com/liustack/tifa"

type Site struct {
	Lang string `json:"lang"`
}

func Layout(c *tifa.Ctx, q Site) (string, error) { return q.Lang, nil }
