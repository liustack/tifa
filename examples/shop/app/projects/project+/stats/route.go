// Package stats is the sales stats page of a project.
package stats

import (
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

// Load reads the project through its Layout first, so an unknown project is a 404. Days start at
// midnight in the time zone of the shop, the one its pages show times in.
func Load(c *tifa.Ctx) (orders.Sales, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return orders.Sales{}, err
	}
	zone, err := time.LoadLocation(p.TimeZone)
	if err != nil {
		return orders.Sales{}, err
	}
	return orders.SalesStats(p.ID, zone), nil
}
