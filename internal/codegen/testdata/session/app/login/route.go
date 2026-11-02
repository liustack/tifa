package login

import "github.com/liustack/tifa"

// Account is the stored session: the browser never sees it whole.
type Account struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Roles []string
}

// Public is the part of the session the browser may read.
type Public struct {
	Name  string  `json:"name"`
	Admin bool    `json:"admin"`
	Boss  *Public `json:"boss"`
}

func (a *Account) Exposed() Public { return Public{Name: a.Email} }

func Load(c *tifa.Ctx) (string, error) { return "x", nil }

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &Account{ID: 1}) }
