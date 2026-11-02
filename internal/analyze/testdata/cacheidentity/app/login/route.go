package login

import "github.com/liustack/tifa"

type User struct{ ID int }

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &User{ID: 1}) }
