package flag

import (
	"github.com/liustack/tifa"

	"example.com/cacheidentity/app/login"
)

const Cache = true

func Load(c *tifa.Ctx) (string, error) {
	if tifa.Session[login.User](c) != nil {
		return "signed in", nil
	}
	return "flag", nil
}
