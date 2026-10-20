package app

import (
	"example.com/muitest/app/admin"
	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (admin.Stats, error) { return admin.Dashboard(), nil }
