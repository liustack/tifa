package docs

import "github.com/liustack/tifa"

// The Layout has a Load but no layout.tsx, and its pages share no Path segment.
func Load(c *tifa.Ctx) (map[string]int, error) { return map[string]int{"v": 1}, nil }
