// Package app is the index of the scenarios.
package app

import "github.com/liustack/tifa"

// Query is the query string of the index. The page does not show it: the parameter exists so that a
// check can ask for the index under a URL the render memo has not seen, which makes the VM render it.
type Query struct {
	After string `json:"after"`
}

type Index struct {
	After string `json:"after"`
}

func Load(c *tifa.Ctx, q Query) (Index, error) {
	return Index{After: q.After}, nil
}
