package journal

import (
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

// Cache keeps the journal list for ten minutes.
const Cache = 10 * time.Minute

func Load(c *tifa.Ctx) ([]Summary, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return nil, err
	}
	return List(l), nil
}
