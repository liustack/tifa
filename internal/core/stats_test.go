package core

import "testing"

type fakeServer struct{ stats Stats }

func (f *fakeServer) Stats() Stats { return f.stats }

// ReadStats reaches the servers that run, the sum of their stats, and none once they stopped.
func TestReadStatsReachesTheRunningServers(t *testing.T) {
	a, b := &fakeServer{stats: Stats{CacheSize: 1, SSR: SSRStats{Renders: 2}}}, &fakeServer{stats: Stats{CacheSize: 10, RenderCache: CacheStats{Hits: 3}}}
	stopA, stopB := Running(a), Running(b)
	if got := ReadStats(); got != (Stats{CacheSize: 11, SSR: SSRStats{Renders: 2}, RenderCache: CacheStats{Hits: 3}}) {
		t.Errorf("both: %+v", got)
	}
	stopA()
	stopA()
	if got := ReadStats(); got != b.stats {
		t.Errorf("after a stopped: %+v", got)
	}
	stopB()
	if got := ReadStats(); got != (Stats{}) {
		t.Errorf("none: %+v", got)
	}
}
