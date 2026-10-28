package products

import (
	"strconv"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// List is the product list page: one page of the search, and two rows below it, the products the
// visitor looked at last and the best rated ones.
type List struct {
	Results
	Seen  []Card `json:"seen"`
	Picks []Card `json:"picks"`
}

// Load searches the catalogue with the filters in the query string. A filter that does not parse
// is left out, as if it were not there.
func Load(c *tifa.Ctx) (List, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return List{}, err
	}
	number := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	return List{
		Results: Search(Filter{
			Query:     c.Query("q"),
			Category:  c.Query("category"),
			Colors:    c.QueryAll("color"),
			Materials: c.QueryAll("material"),
			Badges:    c.QueryAll("badge"),
			Rating:    number(c.Query("rating")),
			Min:       number(c.Query("min")),
			Max:       number(c.Query("max")),
			InStock:   c.Query("stock") == "1",
			Sort:      c.Query("sort"),
			Page:      number(c.Query("page")),
		}, l),
		Seen:  Seen(c, "", l),
		Picks: TopRated(6, l),
	}, nil
}
