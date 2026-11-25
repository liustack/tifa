package team

import "github.com/liustack/tifa"

type Tab struct {
	Tab string `json:"tab"`
}

func Layout(c *tifa.Ctx, q Tab) (string, error) { return c.Param("team") + q.Tab, nil }
