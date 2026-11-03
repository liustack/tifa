package page

import (
	"github.com/liustack/tifa"

	_ "example.com/badsettings/internal/store"
)

func Load(c *tifa.Ctx) (string, error) { return "x", nil }
