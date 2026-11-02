package session

import "github.com/liustack/tifa"

// User is the session of the app. Browser gives the browser the name and nothing else.
type User struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

type Me struct {
	Name string `json:"name"`
}

func (u User) Browser() Me { return Me{Name: u.Name} }

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &User{Name: "Ann", Secret: "x"}) }
