// Package stats tells the tests how often each Load ran and what the hook saw.
package stats

import (
	"github.com/liustack/tifa"

	"example.com/pagecachetest/internal/counter"
)

func GET(c *tifa.Ctx) (counter.Snapshot, error) { return counter.Get(), nil }
