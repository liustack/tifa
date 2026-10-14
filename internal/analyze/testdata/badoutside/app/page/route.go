package page

import (
	"example.com/badoutside/bad"
	"github.com/liustack/tifa"
)

// The directory is the URL.

// Skipped: the imported package does not type check.
func GET(c *tifa.Ctx) error { _ = bad.Bad; return nil }
