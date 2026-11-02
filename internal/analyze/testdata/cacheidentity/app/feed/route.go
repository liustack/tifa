package feed

import (
	"time"

	"github.com/liustack/tifa"

	"example.com/cacheidentity/internal/auth"
)

const Cache = 5 * time.Minute

func Load(c *tifa.Ctx) (string, error) { return auth.Who(c), nil }
