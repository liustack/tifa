package products

import (
	"time"

	"strconv"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// List is the product list page: one page of the search, and a row below it with the best rated
// products.
type List struct {
	Results
	Picks []Card `json:"picks"`
}

// Cache keeps a list for five minutes, a result of a filter in the query string included. Stock
// and ratings change faster than articles do.
const Cache = 5 * time.Minute

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
			Colors:    c.QueryValues("color"),
			Materials: c.QueryValues("material"),
			Badges:    c.QueryValues("badge"),
			Rating:    number(c.Query("rating")),
			Min:       number(c.Query("min")),
			Max:       number(c.Query("max")),
			InStock:   c.Query("stock") == "1",
			Sort:      c.Query("sort"),
			Page:      number(c.Query("page")),
		}, l),
		Picks: TopRated(6, l),
	}, nil
}
