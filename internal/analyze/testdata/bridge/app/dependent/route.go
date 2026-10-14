package dependent

import (
	"example.com/bridge/bridge"
	"github.com/liustack/tifa"
)

// The directory is the URL.

// Skipped: it imports app/bad through bridge, which lives outside app/.
func GET(c *tifa.Ctx) error { _ = bridge.Prefix; return nil }
