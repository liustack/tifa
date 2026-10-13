package dependent

import (
	"example.com/bridge/bridge"
	"github.com/liustack/tifa"
)

const Path = "/dependent"

// Skipped: it imports app/bad through bridge, which lives outside app/.
func OnGet(c *tifa.Ctx) error { _ = bridge.Prefix; return nil }
