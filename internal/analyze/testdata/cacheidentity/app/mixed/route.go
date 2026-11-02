package mixed

import (
	"time"

	"github.com/liustack/tifa"
)

const Cache = time.Minute

// The POST handler of a page with Cache sees the request as it is.
func POST(c *tifa.Ctx) error {
	_, err := c.Cookie("who")
	return err
}
