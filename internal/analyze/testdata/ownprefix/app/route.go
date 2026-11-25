package app

import (
	"github.com/liustack/tifa"

	"github.com/liustack/tifa/examples/ownprefix/filters"
)

// The module path of this app starts with the path of Tifa, as that of the example shop does.
// Its packages are still the app's, and the build reads them.

func Load(c *tifa.Ctx) (string, error) {
	return filters.Sort(c), nil
}
