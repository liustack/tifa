// Package projects is the project workspace Layout, which applies to /projects/{projectId} and
// every page below it.
package projects

import (
	"github.com/liustack/tifa"
	shop "github.com/liustack/tifa/examples/shop"
)

func Load(c *tifa.Ctx) (shop.Project, error) {
	return shop.GetProject(c.Param("projectId"))
}
