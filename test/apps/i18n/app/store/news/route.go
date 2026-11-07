// Package news is a page of the area app/store with the whole-page cache. Its data is the same in
// every language, so only the language of the URL keeps the entries apart.
package news

import (
	"sync/atomic"
	"time"

	"github.com/liustack/tifa"
)

const Cache = time.Minute

// Loads counts the runs of Load, see app/api/loads.
var Loads atomic.Int64

type Data struct {
	Body string `json:"body"`
}

func Load(c *tifa.Ctx) (Data, error) {
	Loads.Add(1)
	return Data{Body: "the same in every language"}, nil
}
