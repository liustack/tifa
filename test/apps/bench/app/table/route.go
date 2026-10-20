// Package table is the table page of eknkc/ssr-benchmark: 1000 rows of two UUIDs each.
package table

import (
	"fmt"
	"math/rand/v2"

	"github.com/liustack/tifa"
)

type Entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// The benchmark makes its rows once per process with crypto.randomUUID. These come from a fixed
// seed, so every run renders the same page.
var entries = func() []Entry {
	r := rand.New(rand.NewPCG(1, 2))
	uuid := func() string {
		var b [16]byte
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		b[6] = b[6]&0x0f | 0x40
		b[8] = b[8]&0x3f | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	}
	out := make([]Entry, 1000)
	for i := range out {
		out[i] = Entry{ID: uuid(), Name: uuid()}
	}
	return out
}()

func Load(c *tifa.Ctx) ([]Entry, error) { return entries, nil }
