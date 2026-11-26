package page

import (
	"example.com/loadwrite/out"
	"github.com/liustack/tifa"
)

// The framework writes the answer of a page: its Load returns data.
func Load(c *tifa.Ctx) (string, error) {
	if err := c.JSON(200, "data"); err != nil {
		return "", err
	}
	return "", out.Write(c)
}
