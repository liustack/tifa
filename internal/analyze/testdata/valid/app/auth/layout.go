package auth

import "github.com/liustack/tifa"

type Session struct {
	User *string `json:"user"`
}

func Load(c *tifa.Ctx) (Session, error) { return Session{}, nil }
