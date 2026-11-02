package plain

import "github.com/liustack/tifa"

// No Cache here, so the cookie is read as usual.
func Load(c *tifa.Ctx) (string, error) {
	ck, _ := c.Cookie("who")
	return c.Header("X-Who") + ck.String(), nil
}
