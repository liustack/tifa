package team

import "github.com/liustack/tifa"

func Layout(c *tifa.Ctx) (string, error) { return c.Param("team") + c.Query("tab"), nil }
