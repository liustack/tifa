package oddvalidate

import "github.com/liustack/tifa"

type Filter struct {
	Page int `json:"page"`
}

func (f Filter) Validate() bool { return true }

func Load(c *tifa.Ctx, q Filter) (string, error) { return "", nil }
