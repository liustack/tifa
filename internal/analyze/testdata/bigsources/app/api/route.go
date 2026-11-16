package api

import (
	"math/rand"
	"time"

	"github.com/liustack/tifa"
)

type Mine struct {
	ID int64 `json:"id"`
}

type User struct{ N uint64 }

type Public struct {
	Number uint64 `json:"number"`
}

func (u *User) Exposed() Public { return Public{Number: rand.Uint64()} }

func GET(c *tifa.Ctx) (Mine, error) { return Mine{ID: time.Now().UnixNano()}, nil }

func POST(c *tifa.Ctx) error { return tifa.SetSession(c, &User{N: 1}) }
