package validated

import "github.com/liustack/tifa"

type Filter struct {
	Page int `json:"page"`
}

// Validate runs once the query is decoded, before Load.
func (f Filter) Validate() error { return nil }

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }

type Section struct {
	Tab string `json:"tab"`
}

// Validate runs with the Ctx of the Layout, so what a Layout may not do, it may not do either.
func (s *Section) Validate(c *tifa.Ctx) error {
	_ = c.Path()
	return nil
}

func Layout(c *tifa.Ctx, q Section) (string, error) { return "", nil }
