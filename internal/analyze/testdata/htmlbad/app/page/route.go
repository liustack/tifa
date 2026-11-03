package page

import "github.com/liustack/tifa"

type Data struct {
	ByHTML map[tifa.HTML]int `json:"byHTML"`
}

func Load(c *tifa.Ctx) (Data, error) { return Data{}, nil }
