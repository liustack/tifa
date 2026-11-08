package journal

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

func Load(c *tifa.Ctx) ([]Summary, error) {
	l, err := storekit.FindLocale(c.Language())
	if err != nil {
		return nil, err
	}
	return List(l), nil
}
