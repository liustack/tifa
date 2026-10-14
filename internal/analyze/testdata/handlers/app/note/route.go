package note

import "github.com/liustack/tifa"

type note struct {
	Text string `json:"text"`
	Tags Tree   `json:"tags"`
	Path List   `json:"path"`
}

// Tree and List refer to themselves through a map and a slice, not through a struct.
type Tree map[string]Tree

type List []List

func POST(c *tifa.Ctx, in note) (note, error) { return in, nil }

func PUT(c *tifa.Ctx, in struct {
	Text string `json:"text"`
}) error {
	return nil
}
