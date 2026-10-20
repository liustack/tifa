package app

import (
	"example.com/muitest/app/admin"
	"github.com/liustack/tifa"
)

type Shell struct {
	User    string `json:"user"`
	Pending int    `json:"pending"`
}

func Layout(c *tifa.Ctx) (Shell, error) {
	return Shell{User: "Riley Chen", Pending: admin.Dashboard().ByStatus["pending"]}, nil
}
