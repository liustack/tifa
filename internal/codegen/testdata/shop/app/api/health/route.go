package health

import "github.com/liustack/tifa"

type Probe struct {
	Deep bool `json:"deep"`
}

type Status struct {
	OK bool `json:"ok"`
}

func GET(c *tifa.Ctx, in Probe) (Status, error) { return Status{OK: true}, nil }

func POST(c *tifa.Ctx) error { return c.NoContent(204) }

func PUT(c *tifa.Ctx) (Status, error) { return Status{OK: true}, nil }

func DELETE(c *tifa.Ctx, in Probe) error { return nil }
