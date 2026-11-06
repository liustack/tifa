// Package news is a page of the area app/store with the whole-page cache. The same page body is
// made for each language, and the entries must never cross.
package news

import (
	"sync/atomic"
	"time"

	"github.com/liustack/tifa"
)

const Cache = time.Minute

var loads atomic.Int64

type Data struct {
	N        int64  `json:"n"`
	Language string `json:"language"`
}

func Load(c *tifa.Ctx) (Data, error) {
	return Data{N: loads.Add(1), Language: c.Language()}, nil
}
