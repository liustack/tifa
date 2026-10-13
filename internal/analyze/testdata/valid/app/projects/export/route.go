package export

import "github.com/liustack/tifa"

// An interface route under the projects layout. It does not share the layout's Path prefix and
// must not shrink it.
const Path = "/exports/projects"

func OnGet(c *tifa.Ctx) error { return c.NoContent(204) }
