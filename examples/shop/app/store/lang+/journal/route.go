package journal

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store"
)

func Load(c *tifa.Ctx) ([]Summary, error) {
	l, err := store.FindLocale(c.Param("lang"))
	if err != nil {
		return nil, err
	}
	return List(l), nil
}
