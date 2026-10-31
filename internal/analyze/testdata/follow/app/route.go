package app

import (
	"strings"

	"github.com/liustack/tifa"
)

func Layout(c *tifa.Ctx) (string, error) { return "root" + strings.Join(c.QueryAll("site"), ","), nil }
