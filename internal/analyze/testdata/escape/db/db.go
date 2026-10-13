package db

import "github.com/liustack/tifa"

type Req struct{ *tifa.Ctx }

// Agent reads a header through the raw request.
func Agent(c *tifa.Ctx) string { return c.Request().UserAgent() }

func (r Req) Flush() { r.Writer().WriteHeader(200) }
