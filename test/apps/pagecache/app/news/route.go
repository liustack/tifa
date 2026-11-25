// Package news is a page with Cache.
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

// Query is the query string of the page.
type Query struct {
	Page string `json:"page"`
}

func Load(c *tifa.Ctx, q Query) (Data, error) {
	return Data{N: counter.NewsLoads.Add(1), Page: q.Page}, nil
}

// POST of a page with Cache is a method handler like any other: it sees the cookie and the session.
func POST(c *tifa.Ctx) error {
	_, err := c.Cookie("visit")
	return c.JSON(200, map[string]bool{"cookie": err == nil, "session": tifa.Session[login.Account](c) != nil})
}
