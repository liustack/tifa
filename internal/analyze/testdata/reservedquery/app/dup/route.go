package dup

import "github.com/liustack/tifa"

// Two fields of one struct with the same name in the query string.

type Query struct {
	Page int    `json:"q"`
	Sort string `json:"q"`
}

func Load(c *tifa.Ctx, q Query) (string, error) {
	return q.Sort, nil
}
