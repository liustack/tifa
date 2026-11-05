package server

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"runtime"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

// The cache charges what its pages hold: a cache filled with distinct pages keeps a live heap
// within cache.size and a small margin, whichever form the requests take.
func TestCacheHeapStaysWithinItsSize(t *testing.T) {
	const size = 8 << 20
	words := strings.Fields("order shipped paid refund customer address parcel invoice coupon stock price total tax note")
	page := func(in ssr.Input) string {
		h := fnv.New64a()
		h.Write([]byte(in.URL))
		r := rand.New(rand.NewPCG(h.Sum64(), 1))
		var b strings.Builder
		b.WriteString("<html>" + in.URL)
		for b.Len() < 100<<10 {
			fmt.Fprintf(&b, `<tr><td>%s-%d</td><td>%s</td></tr>`, words[r.IntN(len(words))], r.IntN(100000), words[r.IntN(len(words))])
		}
		return b.String() + "</html>"
	}
	for _, accept := range []string{"gzip", "zstd", ""} {
		t.Run("accept="+accept, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cache.Size = size
			app := frontApp()
			s := newServer(cfg, app.Hooks)
			s.register(app.Layouts, app.Routes)
			render := htmlOnly(func(_ context.Context, in ssr.Input) (string, error) { return page(in), nil })
			if err := s.setFront(dist, render); err != nil {
				t.Fatal(err)
			}
			live := func() uint64 {
				runtime.GC()
				runtime.GC()
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				return m.HeapAlloc
			}
			for i := range 20 { // the compressors and their pools are there for good once used
				get(s, fmt.Sprintf("/projects/warm%d", i), "Accept-Encoding", accept)
			}
			base := live()
			for i := range 400 {
				if w := get(s, fmt.Sprintf("/projects/p%d", i), "Accept-Encoding", accept); w.Code != 200 {
					t.Fatalf("page %d: %d", i, w.Code)
				}
			}
			grown := int64(live()) - int64(base)
			s.cache.mu.Lock()
			used, entries := s.cache.used, len(s.cache.pages)
			s.cache.mu.Unlock()
			t.Logf("accept %q: %d pages kept, charged %d, heap grew by %d of %d", accept, entries, used, grown, size)
			if used > size || grown > size+size/8 {
				t.Errorf("charged %d, heap grew by %d, cache.size %d", used, grown, size)
			}
			if entries < 10 {
				t.Errorf("%d pages kept", entries)
			}
		})
	}
}
