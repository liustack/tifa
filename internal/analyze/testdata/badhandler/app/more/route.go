package more

import "github.com/liustack/tifa"

type Iface struct {
	V any `json:"v"`
}

type Deep struct {
	Inner struct {
		N string `form:"n"`
	} `json:"inner"`
}

func GET(c *tifa.Ctx, in struct{ Data []byte }) error { return nil }

func POST(c *tifa.Ctx, in Iface) error { return nil }

func PUT(c *tifa.Ctx) (chan int, error) { return nil, nil }

func PATCH(c *tifa.Ctx, in Deep) error { return nil }

func DELETE(c *tifa.Ctx, a, b string) error { return nil }
