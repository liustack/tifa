package core

import (
	"slices"
	"testing"
)

type fakeServer struct {
	expired [][]string
	stats   Stats
}

func (f *fakeServer) ExpirePages(paths []string) { f.expired = append(f.expired, paths) }
func (f *fakeServer) Stats() Stats               { return f.stats }

// ExpirePages and ReadStats reach the servers that run, the sum of their stats, and none once they
// stopped.
func TestExpirePagesAndReadStatsReachTheRunningServers(t *testing.T) {
	a, b := &fakeServer{stats: Stats{CacheSize: 1, SSR: SSRStats{Renders: 2}}}, &fakeServer{stats: Stats{CacheSize: 10, PageCache: CacheStats{Hits: 3}}}
	ExpirePages([]string{"/before"})
	stopA, stopB := Running(a), Running(b)
	ExpirePages([]string{"/a", "/b"})
	if got := ReadStats(); got != (Stats{CacheSize: 11, SSR: SSRStats{Renders: 2}, PageCache: CacheStats{Hits: 3}}) {
		t.Errorf("both: %+v", got)
	}
	stopA()
	stopA()
	ExpirePages([]string{"/c"})
	if got := ReadStats(); got != b.stats {
		t.Errorf("after a stopped: %+v", got)
	}
	stopB()
	if got := ReadStats(); got != (Stats{}) {
		t.Errorf("none: %+v", got)
	}
	if !slices.EqualFunc(a.expired, [][]string{{"/a", "/b"}}, slices.Equal) || !slices.EqualFunc(b.expired, [][]string{{"/a", "/b"}, {"/c"}}, slices.Equal) {
		t.Errorf("a got %q, b got %q", a.expired, b.expired)
	}
}
