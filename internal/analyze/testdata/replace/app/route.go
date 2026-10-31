package app

import "github.com/liustack/tifa"

// The root Layout renders only in the browser, which the pages below layout_.tsx do not inherit.
const LayoutSSR = false

func Layout(c *tifa.Ctx) (string, error) { return "workbench", nil }
