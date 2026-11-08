// Package blog is the list of posts of a language, with Cache.
package blog

import (
	"time"

	"github.com/liustack/tifa"

	"example.com/contenttest"
	"example.com/contenttest/app/lang+"
)

type List struct {
	Posts []contenttest.Post `json:"posts"`
}

const Cache = 10 * time.Minute

func Load(c *tifa.Ctx) (List, error) {
	s, err := tifa.Use(c, lang.Layout)
	if err != nil {
		return List{}, err
	}
	return List{Posts: Published(s.Lang)}, nil
}
