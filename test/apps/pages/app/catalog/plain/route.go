// Package plain is the page of catalog rendered only in the browser.
package plain

import (
	"example.com/pagestest/app/catalog"
	"github.com/liustack/tifa"
)

// SSR = false renders the page only in the browser.
const SSR = false

func Load(c *tifa.Ctx, f catalog.Filter) (catalog.Shown, error) { return catalog.Shown(f), nil }
