package auth

import "github.com/liustack/tifa"

type Session struct {
	User *string `json:"user"`
}

// Next is where the visitor goes after signing in, a query parameter of every page below.
type Next struct {
	Next string `json:"next"`
}

func Layout(c *tifa.Ctx, q Next) (Session, error) { return Session{}, nil }
