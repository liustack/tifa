// Package store is the page of the area app/store, which is in zh-CN and en-US.
package store

import (
	"github.com/liustack/tifa"
)

type Data struct {
	// Language is what c.Language gave in Load, Q the query parameter q.
	Language string `json:"language"`
	Q        string `json:"q"`
}

func Load(c *tifa.Ctx) (Data, error) {
	return Data{Language: c.Language(), Q: c.Query("q")}, nil
}
