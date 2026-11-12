package core

import (
	"reflect"
	"testing"
)

type fakeServer struct{ stats Stats }

func (f *fakeServer) Stats() Stats { return f.stats }

// ReadStats reaches the servers that run, the sum of their stats, and none once they stopped.
func TestReadStatsReachesTheRunningServers(t *testing.T) {
	a, b := &fakeServer{stats: Stats{MemoSize: 1, SSR: SSRStats{Renders: 2}}}, &fakeServer{stats: Stats{MemoSize: 10, Memo: MemoStats{Hits: 3}}}
	stopA, stopB := Running(a), Running(b)
	if got := ReadStats(); !reflect.DeepEqual(got, Stats{MemoSize: 11, SSR: SSRStats{Renders: 2}, Memo: MemoStats{Hits: 3}}) {
		t.Errorf("both: %+v", got)
	}
	stopA()
	stopA()
	if got := ReadStats(); !reflect.DeepEqual(got, b.stats) {
		t.Errorf("after a stopped: %+v", got)
	}
	stopB()
	if got := ReadStats(); !reflect.DeepEqual(got, Stats{}) {
		t.Errorf("none: %+v", got)
	}
}

// The counts of the render memo by route add up route by route over the servers that run, and the
// sum leaves the maps of the servers as they were.
func TestReadStatsAddsTheRoutesOfTheMemo(t *testing.T) {
	a := &fakeServer{stats: Stats{Memo: MemoStats{Hits: 1, Misses: 2, Routes: map[string]MemoRouteStats{
		"/a": {Hits: 1, Misses: 1},
		"":   {Misses: 1},
	}}}}
	b := &fakeServer{stats: Stats{Memo: MemoStats{Hits: 2, Shared: 1, Routes: map[string]MemoRouteStats{
		"/a": {Hits: 2, Shared: 1},
	}}}}
	defer Running(a)()
	defer Running(b)()
	want := MemoStats{Hits: 3, Misses: 2, Shared: 1, Routes: map[string]MemoRouteStats{
		"/a": {Hits: 3, Misses: 1, Shared: 1},
		"":   {Misses: 1},
	}}
	if got := ReadStats().Memo; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if a.stats.Memo.Routes["/a"] != (MemoRouteStats{Hits: 1, Misses: 1}) {
		t.Errorf("the sum changed the stats of a server: %+v", a.stats.Memo.Routes)
	}
}

// Unchanged adds up like the other counts, in all and by route.
func TestReadStatsAddsTheUnchangedPages(t *testing.T) {
	a := &fakeServer{stats: Stats{Memo: MemoStats{Misses: 2, Unchanged: 1, Routes: map[string]MemoRouteStats{"/a": {Misses: 2, Unchanged: 1}}}}}
	b := &fakeServer{stats: Stats{Memo: MemoStats{Misses: 3, Unchanged: 2, Routes: map[string]MemoRouteStats{"/a": {Misses: 3, Unchanged: 2}}}}}
	defer Running(a)()
	defer Running(b)()
	want := MemoStats{Misses: 5, Unchanged: 3, Routes: map[string]MemoRouteStats{"/a": {Misses: 5, Unchanged: 3}}}
	if got := ReadStats().Memo; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
