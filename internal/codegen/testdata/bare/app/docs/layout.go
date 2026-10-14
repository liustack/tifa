package docs

import "github.com/liustack/tifa"

// The Layout has a Layout function but no layout.tsx.
func Layout(c *tifa.Ctx) (map[string]int, error) { return map[string]int{"v": 1}, nil }
