// Package login signs a visitor in. It is on a route without Cache, as a sign-in must be.
package login

import "github.com/liustack/tifa"

type Account struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Me struct {
	Name string `json:"name"`
}

func (a Account) Exposed() Me { return Me{Name: a.Name} }

type Credentials struct {
	Name string `json:"name"`
}

func POST(c *tifa.Ctx, in Credentials) error {
	return tifa.SetSession(c, &Account{ID: 1, Name: in.Name})
}
