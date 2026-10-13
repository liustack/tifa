package overview

import (
	"slices"

	"example.com/pagestest/app/shop"
	"github.com/liustack/tifa"
)

const Path = "/shops/{shopId}"

// The overview depends only on the shop and the query, so its HTML is cached.
const Cache = true

type Overview struct {
	Shop  string   `json:"shop"`
	Items []string `json:"items"`
}

func Load(c *tifa.Ctx) (Overview, error) {
	s, err := tifa.Use(c, shop.Load)
	if err != nil {
		return Overview{}, err
	}
	items := []string{"apple"}
	if c.Query("sort") == "desc" {
		slices.Reverse(items)
	}
	return Overview{Shop: s.Name, Items: items}, nil
}
