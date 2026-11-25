package products

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// List is the product list page: one page of the search, and a row below it with the best rated
// products.
type List struct {
	Results
	Picks []Card `json:"picks"`
}

// Query is the query string of the page: the filters, the sort and the page, from 1. The names are
// the ones the links of the page write.
type Query struct {
	Q        string   `json:"q"`
	Category string   `json:"category"`
	Color    []string `json:"color"`
	Material []string `json:"material"`
	Badge    []string `json:"badge"`
	Rating   int      `json:"rating"`
	Min      int      `json:"min"`
	Max      int      `json:"max"`
	Stock    bool     `json:"stock"`
	Sort     string   `json:"sort"`
	Page     int      `json:"page"`
}

// Load searches the catalogue with the filters in the query string. A number that is not a whole
// number answers 400, and one below 0 counts as 0.
func Load(c *tifa.Ctx, q Query) (List, error) {
	l, err := storekit.FindLocale(c.Language())
	if err != nil {
		return List{}, err
	}
	return List{
		Results: Search(Filter{
			Query:     q.Q,
			Category:  q.Category,
			Colors:    q.Color,
			Materials: q.Material,
			Badges:    q.Badge,
			Rating:    max(q.Rating, 0),
			Min:       max(q.Min, 0),
			Max:       max(q.Max, 0),
			InStock:   q.Stock,
			Sort:      q.Sort,
			Page:      max(q.Page, 0),
		}, l),
		Picks: TopRated(6, l),
	}, nil
}
