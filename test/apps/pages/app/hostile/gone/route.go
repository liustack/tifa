package gone

import "github.com/liustack/tifa"

// Load fails, so the page of /hostile/gone is an error page, with the data of the Layout above it.
func Load(c *tifa.Ctx) (string, error) {
	return "", tifa.ErrNotFound
}
