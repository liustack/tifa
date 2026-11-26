package page

import "github.com/liustack/tifa"

// The directory is the URL.

type loader struct{}

func (loader) Load(c *tifa.Ctx) (int, error) { return 1, nil }

func Load(c *tifa.Ctx) (int, error) {
	a, _ := tifa.Use(c, func(c *tifa.Ctx) (int, error) { return 1, nil })
	b, _ := tifa.Use(c, loader{}.Load)
	// A field that holds a function is a function value: the build cannot see which one it holds.
	d, _ := tifa.Use(c, held.fn)
	return a + b + d, nil
}

type deps struct{ fn func(*tifa.Ctx) (int, error) }

var held = deps{fn: count}

func count(c *tifa.Ctx) (int, error) { return 1, nil }
