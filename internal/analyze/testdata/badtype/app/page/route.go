package page

import "github.com/liustack/tifa"

const Path = "/page"

type Data struct {
	Name    string         `json:"name"`
	Updates chan int       `json:"updates"`
	ByID    map[int]string `json:"byId"`
	Items   []Item         `json:"items"`
}

type Item struct {
	Extra any `json:"extra"`
}

func Load(c *tifa.Ctx) (Data, error) { return Data{}, nil }
