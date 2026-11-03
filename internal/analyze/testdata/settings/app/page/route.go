package page

import (
	"github.com/liustack/tifa"

	"example.com/settings"
)

func Load(c *tifa.Ctx) (string, error) { return settings.DBURL, nil }
