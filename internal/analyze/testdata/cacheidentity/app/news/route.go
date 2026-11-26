package news

import (
	"time"

	"github.com/liustack/tifa"

	"example.com/cacheidentity/app/login"
	"example.com/cacheidentity/internal/auth"
)

const Cache = time.Minute

func Load(c *tifa.Ctx) (string, error) {
	if tifa.Session[login.User](c) != nil {
		return "signed in", nil
	}
	return "news" + auth.Lang(c), nil
}
