package float32

import "github.com/liustack/tifa"

type Weight float32

type Filter struct {
	Price  float32   `json:"price"`
	Prices []float32 `json:"prices"`
	Weight Weight    `json:"weight"`
	Ok     float64   `json:"ok"`
}

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
