package quit

import "github.com/liustack/tifa"

// POST has no input and no result: the session still reaches the browser.
func POST(c *tifa.Ctx) error { return tifa.ClearSession(c) }
