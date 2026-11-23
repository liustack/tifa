package app

import "github.com/liustack/tifa"

// The root page renders only in the browser, which the pages below layout_.tsx do not share.
const SSR = false

func Layout(c *tifa.Ctx) (string, error) { return "workbench", nil }
