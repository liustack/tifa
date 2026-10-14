package mixed

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) { return "page", nil }

func Layout(c *tifa.Ctx) (string, error) { return "layout", nil }

const LayoutSSR = false
