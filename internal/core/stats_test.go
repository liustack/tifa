package core

import "testing"

type fakeServer struct{ stats Stats }

func (f *fakeServer) Stats() Stats { return f.stats }

// ReadStats reaches the servers that run, the sum of their stats, and none once they stopped.
func TestReadStatsReachesTheRunningServers(t *testing.T) {
	a, b := &fakeServer{stats: Stats{MemoSize: 1, SSR: SSRStats{Renders: 2, Replaced: 1, Memory: 100}}}, &fakeServer{stats: Stats{MemoSize: 10, SSR: SSRStats{Replaced: 4, Memory: 20}, Memo: MemoStats{Hits: 3}}}
	stopA, stopB := Running(a), Running(b)
	if got := ReadStats(); got != (Stats{MemoSize: 11, SSR: SSRStats{Renders: 2, Replaced: 5, Memory: 120}, Memo: MemoStats{Hits: 3}}) {
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
