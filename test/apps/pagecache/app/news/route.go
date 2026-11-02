// Package news is a page with the whole-page cache. Its Load reads only a query parameter, which
// the cache keeps in its key.
package news

import (
	"time"

	"github.com/liustack/tifa"

	"example.com/pagecachetest/app/login"
	"example.com/pagecachetest/internal/counter"
)

const Cache = time.Minute

type Data struct {
	N    int64  `json:"n"`
	Page string `json:"page"`
}

func Load(c *tifa.Ctx) (Data, error) {
	return Data{N: counter.NewsLoads.Add(1), Page: c.Query("page")}, nil
}

// POST of a page with Cache is a method handler like any other: it sees the cookie and the session.
func POST(c *tifa.Ctx) error {
	_, err := c.Cookie("visit")
	return c.JSON(200, map[string]bool{"cookie": err == nil, "session": tifa.Session[login.Account](c) != nil})
}
