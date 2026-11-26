// Package catalog is a list filtered by its query string, whose links are built from the query of
// the page.
package catalog

import "github.com/liustack/tifa"

// Filter is the query string of the page.
type Filter struct {
	Q    string  `json:"q"`
	Min  float64 `json:"min"`
	Page int     `json:"page"`
}

// Shown is the filter the Load was given.
type Shown struct {
	Q    string  `json:"q"`
	Min  float64 `json:"min"`
	Page int     `json:"page"`
}

func Load(c *tifa.Ctx, f Filter) (Shown, error) {
	return Shown(f), nil
}
