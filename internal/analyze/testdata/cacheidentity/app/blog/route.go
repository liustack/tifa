package blog

import (
	"time"

	"github.com/liustack/tifa"
)

const Cache = time.Minute

// Every page this Layout wraps has Cache, so it runs for anonymous requests only.
func Layout(c *tifa.Ctx) (string, error) {
	ck, err := c.Cookie("theme")
	if err != nil {
		return "", nil
	}
	return ck.Value, nil
}
