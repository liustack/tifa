// Package fast is a page with a cache time of two seconds, whose Load does what the test asks for.
package fast

import (
	"errors"
	"time"

	"github.com/liustack/tifa"

	"example.com/pagecachetest/internal/counter"
)

const Cache = 2 * time.Second

type Data struct {
	N int64 `json:"n"`
}

func Load(c *tifa.Ctx) (Data, error) {
	n := counter.FastLoads.Add(1)
	switch counter.Mode() {
	case "slow":
		time.Sleep(600 * time.Millisecond)
	case "error":
		return Data{}, errors.New("the backend is down")
	case "notfound":
		return Data{}, tifa.ErrNotFound
	case "redirect":
		return Data{}, tifa.Redirect("/news", 302)
	}
	return Data{N: n}, nil
}
