// Package mine is an API route of the area app/store.
package mine

import "github.com/liustack/tifa"

type Out struct {
	Language string `json:"language"`
	URL      string `json:"url"`
}

func GET(c *tifa.Ctx) (Out, error) {
	return Out{Language: c.Language(), URL: c.Request().URL.RequestURI()}, nil
}
