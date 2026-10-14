package note

import "github.com/liustack/tifa"

type Query struct {
	Tag   string   `json:"tag"`
	Tags  []string `json:"tags"`
	Limit int      `json:"limit,omitempty"`
}

// Note refers to itself, so the module declares it once for the input and the result.
type Note struct {
	Text string `json:"text"`
	Next *Note  `json:"next"`
}

func GET(c *tifa.Ctx, in Query) ([]Note, error) { return nil, nil }

func POST(c *tifa.Ctx, in Note) (Note, error) { return in, nil }

func PUT(c *tifa.Ctx) error { return c.NoContent(204) }

func DELETE(c *tifa.Ctx, in Query) error { return nil }
