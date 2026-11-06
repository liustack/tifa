// Package journal answers the entry hello of the journal in the language of the route, an API
// route of the area app/store.
package journal

import (
	"github.com/liustack/tifa"

	i18ntest "example.com/i18ntest"
)

type Data struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func GET(c *tifa.Ctx) (Data, error) {
	e, err := i18ntest.Journal.Get(c.Language(), "hello")
	if err != nil {
		return Data{}, err
	}
	return Data{Title: e.Title, Body: string(e.Body())}, nil
}
