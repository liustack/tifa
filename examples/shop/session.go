package shop

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Session is who is signed in to the workbench. tifa keeps it on the server, in the memory of the
// process: the shop does not call SetSessionStore, so sessions need no setup and a restart signs
// everyone out.
type Session struct {
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Initials string `json:"initials"`
}

// Me is the part of the session the browser may keep.
type Me struct {
	Name     string `json:"name"`
	Initials string `json:"initials"`
}

func (s Session) Exposed() Me { return Me{Name: s.Name, Initials: s.Initials} }

// account is a user of the workbench. The password is kept as a bcrypt hash, as a real shop keeps
// it in its database.
type account struct {
	id       string
	email    string
	hash     []byte
	name     string
	initials string
}

// The demo account, also written in the README of the shop.
const (
	DemoEmail    = "owner@shanhai.example"
	DemoPassword = "shanhai-demo"
)

// demoHash is the bcrypt hash (cost 10) of DemoPassword.
const demoHash = "$2a$10$qW79U3ecT310MNCRYFWRguqtka9ZoeqB6TouvM1rRTV2L3yM66xWO"

var accounts = []account{
	{id: "u-1", email: DemoEmail, hash: []byte(demoHash), name: "顾遥", initials: "顾"},
}

// CheckPassword returns the session of the account with this e-mail and password, and false for
// a wrong pair. An unknown e-mail is checked against the demo hash, so it takes as long as a
// wrong password.
func CheckPassword(email, password string) (Session, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	var found *account
	for i := range accounts {
		if accounts[i].email == email {
			found = &accounts[i]
		}
	}
	hash := []byte(demoHash)
	if found != nil {
		hash = found.hash
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil || found == nil {
		return Session{}, false
	}
	return Session{UserID: found.id, Name: found.name, Initials: found.initials}, true
}
