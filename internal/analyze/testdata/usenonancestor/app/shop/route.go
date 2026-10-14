package shop

import (
	"example.com/usenonancestor/app/admin"
	"github.com/liustack/tifa"
)

// The directory is the URL.

func Load(c *tifa.Ctx) (string, error) {
	return tifa.Use(c, admin.Layout)
}
