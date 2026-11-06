// Package loads tells the tests how often the Load of app/store/news ran.
package loads

import (
	"github.com/liustack/tifa"

	"example.com/i18ntest/app/store/news"
)

type Out struct {
	News int64 `json:"news"`
}

func GET(c *tifa.Ctx) (Out, error) { return Out{News: news.Loads.Load()}, nil }
