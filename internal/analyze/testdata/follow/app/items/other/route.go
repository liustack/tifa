package other

import (
	"example.com/follow/db"
	"github.com/liustack/tifa"
)

const Path = "/other"

// Calls the same helper as list. Only what this Load reaches is collected for it.
func Load(c *tifa.Ctx) (string, error) { return db.Sort(c), nil }
