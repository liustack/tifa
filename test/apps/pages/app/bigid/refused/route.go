// Package refused sends an id a browser would round, which tifa refuses at run time.
package refused

import "github.com/liustack/tifa"

type Row struct {
	// ID is 2^53+1 and has no `json:",string"`, so the page fails with an error.
	ID int64 `json:"id"`
}

func Load(c *tifa.Ctx) (Row, error) {
	return Row{ID: 9007199254740993}, nil
}
