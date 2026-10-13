package page

import (
	"example.com/badoutside/bad"
	"github.com/liustack/tifa"
)

const Path = "/page"

// Skipped: the imported package does not type check.
func OnGet(c *tifa.Ctx) error { _ = bad.Bad; return nil }
