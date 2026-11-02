package logout

import "github.com/liustack/tifa"

type SignOut struct {
	Everywhere bool `json:"everywhere"`
}

func POST(c *tifa.Ctx, in SignOut) error { return tifa.ClearSession(c) }
