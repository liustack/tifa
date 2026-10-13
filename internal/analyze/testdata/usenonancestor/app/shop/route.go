package shop

import (
	"example.com/usenonancestor/app/admin"
	"github.com/liustack/tifa"
)

const Path = "/shop"

func Load(c *tifa.Ctx) (string, error) {
	return tifa.Use(c, admin.Load)
}
