// Package bigid carries an id above 2^53, which a JavaScript number cannot hold.
package bigid

import "github.com/liustack/tifa"

type Row struct {
	// ID is 2^53+1, sent as a string with `json:",string"`, so no digit is lost.
	ID int64 `json:"id,string"`
	// Stamp is a time in milliseconds, below 2^53, so it stays a number.
	Stamp int64 `json:"stamp"`
	// Big is a float64, a double already, so it stays a number whatever its size.
	Big float64 `json:"big"`
}

func Load(c *tifa.Ctx) (Row, error) {
	return Row{ID: 9007199254740993, Stamp: 1763300000123, Big: 1e17}, nil
}
