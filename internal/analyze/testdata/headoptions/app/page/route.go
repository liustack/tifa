package page

import "github.com/liustack/tifa"

type Form struct {
	Title string `json:"title"`
}

func Load(c *tifa.Ctx) (string, error) { return "", nil }

func POST(c *tifa.Ctx, in Form) error { return nil }

func OPTIONS(c *tifa.Ctx) error { return c.NoContent(204) }
