package login

import "github.com/liustack/tifa"

// Account is the session: the secret stays on the server.
type Account struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

// Me is what the browser may keep.
type Me struct {
	Name string `json:"name"`
}

func (a Account) Browser() Me { return Me{Name: a.Name} }

type Credentials struct {
	Name string `json:"name"`
}

func POST(c *tifa.Ctx, in Credentials) error {
	if in.Name == "" {
		return tifa.Error{Status: 400, Message: "name is required"}
	}
	return tifa.SetSession(c, &Account{ID: 1, Name: in.Name, Secret: "never sent to the browser"})
}
