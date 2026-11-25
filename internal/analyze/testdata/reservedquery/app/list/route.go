package list

import "github.com/liustack/tifa"

// The directory is the URL.

type Query struct {
	Page string `json:"page"`
	Tifa string `json:"_tifa"`
}

func Load(c *tifa.Ctx, q Query) (string, error) {
	return q.Page + q.Tifa, nil
}
