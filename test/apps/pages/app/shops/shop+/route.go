package shop

import (
	"slices"

	"github.com/liustack/tifa"
)

type Overview struct {
	Shop  string   `json:"shop"`
	Items []string `json:"items"`
}

func Load(c *tifa.Ctx) (Overview, error) {
	s, err := tifa.Use(c, Layout)
	if err != nil {
		return Overview{}, err
	}
	items := []string{"apple"}
	if c.Query("sort") == "desc" {
		slices.Reverse(items)
	}
	return Overview{Shop: s.Name, Items: items}, nil
}

func Layout(c *tifa.Ctx) (Shop, error) {
	id := c.Param("shop")
	if id == "closed" {
		return Shop{}, tifa.ErrNotFound
	}
	return Shop{ID: id, Name: "Shop " + id}, nil
}
