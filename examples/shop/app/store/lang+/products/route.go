package products

import (
	"strconv"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// Load searches the catalogue with the filters in the query string. A filter that does not parse
// is left out, as if it were not there.
func Load(c *tifa.Ctx) (Results, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return Results{}, err
	}
	number := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	return Search(Filter{
		Query:    c.Query("q"),
		Category: c.Query("category"),
		Colors:   c.QueryAll("color"),
		Min:      number(c.Query("min")),
		Max:      number(c.Query("max")),
		InStock:  c.Query("stock") == "1",
		Sort:     c.Query("sort"),
		Page:     number(c.Query("page")),
	}, l), nil
}
