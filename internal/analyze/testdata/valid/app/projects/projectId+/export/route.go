package export

import "github.com/liustack/tifa"

// An API route under the project layout.
func GET(c *tifa.Ctx) error { return c.NoContent(204) }
