package files

import "github.com/liustack/tifa"

type Entry struct {
	Name     string  `json:"name"`
	Children []Entry `json:"children"`
}

func Load(c *tifa.Ctx) (Entry, error) {
	return Entry{Name: c.Param("path") + c.Query("rev")}, nil
}
