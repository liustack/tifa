package note

import "github.com/liustack/tifa"

type note struct {
	Text string `json:"text"`
}

func POST(c *tifa.Ctx, in note) (note, error) { return in, nil }

func PUT(c *tifa.Ctx, in struct {
	Text string `json:"text"`
}) error {
	return nil
}
