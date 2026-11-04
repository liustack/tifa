package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// Stats counts the hits, misses and shared makes of both caches, the old pages sent for failed
// ones, and the entries and bytes they keep.
func TestStatsCountTheCaches(t *testing.T) {
	e := newPageEnv(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) {
		once.Do(func() { close(started) })
		<-release
		return n, nil
	})
	var wg sync.WaitGroup
	wg.Go(func() { get(e.s, "/news") })
	<-started
	for range 2 {
		wg.Go(func() { get(e.s, "/news") })
	}
	waitShared := time.Now().Add(5 * time.Second)
	for e.s.Stats().PageCache.Shared != 2 && time.Now().Before(waitShared) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	get(e.s, "/news")
	get(e.s, "/stats")
	get(e.s, "/stats")
	e.clock.advance(61 * time.Second)
	e.setLoad(func(c *tifa.Ctx, n int64) (any, error) { return nil, errors.New("database down") })
	get(e.s, "/news")
	s := e.s.Stats()
	// /news: one miss, two shared, one hit, one miss that failed. Its render and the two of /stats
	// went through the render cache.
	if s.PageCache != (tifa.CacheStats{Entries: 1, Hits: 1, Misses: 2, Shared: 2, Stale: 1}) {
		t.Errorf("whole-page cache %+v", s.PageCache)
	}
	if s.RenderCache != (tifa.CacheStats{Entries: 2, Hits: 1, Misses: 2}) {
		t.Errorf("render cache %+v", s.RenderCache)
	}
	if s.CacheBytes <= 0 || s.CacheSize != e.s.cache.size || s.SSR != (tifa.SSRStats{}) {
		t.Errorf("%+v", s)
	}
}

// tifa.Stats reaches the server of the program from OnStart on, and nothing once it stopped.
func TestStatsReachTheRunningServer(t *testing.T) {
	var during tifa.ServerStats
	app := App{Hooks: Hooks{OnStart: func(context.Context) error {
		during = tifa.Stats()
		return errStop
	}}}
	if _, _, err := runApp(t, context.Background(), app, nil, "--server-addr", "127.0.0.1:0", "--cache-size", "1MiB"); !errors.Is(err, errStop) {
		t.Fatalf("serve: %v", err)
	}
	if during.CacheSize != 1<<20 {
		t.Errorf("during OnStart: %+v", during)
	}
	if after := tifa.Stats(); after != (tifa.ServerStats{}) {
		t.Errorf("after the stop: %+v", after)
	}
}
