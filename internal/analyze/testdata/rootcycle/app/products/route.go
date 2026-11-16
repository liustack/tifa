package products

import (
	"github.com/liustack/tifa"

	"example.com/rootcycle"
)

var Names = []string{"a"}

func Load(c *tifa.Ctx) ([]string, error) { return rootcycle.Names, nil }
