package dependent

import (
	"example.com/typeerror/app/page"
	"github.com/liustack/tifa"
)

// This package imports a package with a type error, so the analyzer skips it and does not
// report its OnGet next to route.tsx.
const Path = page.Path + "/dependent"

func OnGet(c *tifa.Ctx) error { return nil }
