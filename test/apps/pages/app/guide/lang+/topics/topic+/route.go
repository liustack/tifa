package topic

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) (string, error) {
	if c.Param("topic") != "tea" {
		return "", tifa.ErrNotFound
	}
	return "tea", nil
}
