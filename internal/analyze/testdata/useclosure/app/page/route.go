package page

import "github.com/liustack/tifa"

// The directory is the URL.

type loader struct{}

func (loader) Load(c *tifa.Ctx) (int, error) { return 1, nil }

func Load(c *tifa.Ctx) (int, error) {
	a, _ := tifa.Use(c, func(c *tifa.Ctx) (int, error) { return 1, nil })
	b, _ := tifa.Use(c, loader{}.Load)
	return a + b, nil
}
