package photos

import (
	"time"

	"github.com/liustack/tifa"
)

func Load(c *tifa.Ctx) (string, error) { return "", nil }

func POST(c *tifa.Ctx) error { return nil }

const BodyLimit = 8 << 20

var ReadTimeout = 5 * time.Minute
