package files

import "github.com/liustack/tifa"

type Entry struct {
	Name     string  `json:"name"`
	Children []Entry `json:"children"`
}

type Rev struct {
	Rev string `json:"rev"`
}

func Load(c *tifa.Ctx, q Rev) (Entry, error) {
	return Entry{Name: c.Param("path") + q.Rev}, nil
}
