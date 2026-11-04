package limits

import (
	"time"

	"github.com/liustack/tifa"
)

const BodyLimit = 0

var ReadTimeout = 5 * time.Minute.Seconds()

func POST(c *tifa.Ctx) error { return nil }
