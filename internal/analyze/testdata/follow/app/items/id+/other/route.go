package other

import (
	"example.com/follow/db"
	"github.com/liustack/tifa"
)

// The directory is the URL.

// Calls a helper like list does. Only what this Load reaches is collected for it: it does not
// use the Layout.
func Load(c *tifa.Ctx) (string, error) { return db.Sort(c), nil }
