package dependent

import (
	"example.com/typeerror/app/page"
	"github.com/liustack/tifa"
)

// This package imports a package with a type error, so the analyzer skips it and does not
// report its GET next to route.tsx.
var _ = page.Name

func GET(c *tifa.Ctx) error { return nil }
