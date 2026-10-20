package order

import (
	"example.com/muitest/app/admin"
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (admin.Order, error) { return admin.Get(c.Param("order")) }
