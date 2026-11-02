// Package account is a page without Cache that needs the session.
package account

import (
	"github.com/liustack/tifa"

	"example.com/pagecachetest/app/login"
)

type Data struct {
	Name string `json:"name"`
}

func Load(c *tifa.Ctx) (Data, error) {
	if a := tifa.Session[login.Account](c); a != nil {
		return Data{Name: a.Name}, nil
	}
	return Data{}, nil
}
