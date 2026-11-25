package validated

import "github.com/liustack/tifa"

type Filter struct {
	Page int `json:"page"`
}

func (f Filter) Validate() error { return nil }

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
