// Package mode sets what the Load of the fast page does.
package mode

import (
	"github.com/liustack/tifa"

	"example.com/pagecachetest/internal/counter"
)

type Mode struct {
	Mode string `json:"mode"`
}

func POST(c *tifa.Ctx, in Mode) error {
	counter.SetMode(in.Mode)
	return nil
}
