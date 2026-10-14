package projects

import "github.com/liustack/tifa"

func Layout(c *tifa.Ctx) (string, error) {
	return c.Param("projectId"), nil
}
