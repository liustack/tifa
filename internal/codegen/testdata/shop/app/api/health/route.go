package health

import "github.com/liustack/tifa"

const Path = "/api/health"

func OnGet(c *tifa.Ctx) error { return c.Text(200, "ok") }

func OnPost(c *tifa.Ctx) error { return c.NoContent(204) }
