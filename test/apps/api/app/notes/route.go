// Package notes answers with typed handlers: the input comes from the query string or the body,
// and the result is written as JSON.
package notes

import (
	"errors"

	"github.com/liustack/tifa"
)

type Query struct {
	Tag   string `json:"tag"`
	Limit int    `json:"limit"`
}

type Note struct {
	Text string `json:"text"`
	Tag  string `json:"tag"`
}

func (n *Note) Validate() error {
	switch {
	case n.Text == "":
		return tifa.Fields{"text": "write something"}
	case len(n.Text) > 20:
		return errors.New("text is too long")
	}
	return nil
}

func GET(c *tifa.Ctx, in Query) ([]Note, error) {
	notes := []Note{{Text: "hi", Tag: in.Tag}}
	return notes[:min(in.Limit, len(notes))], nil
}

func POST(c *tifa.Ctx, in Note) (Note, error) { return in, nil }

func DELETE(c *tifa.Ctx, in Query) error { return nil }
