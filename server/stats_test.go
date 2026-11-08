package server

import (
	"context"
	"errors"
	"testing"

	"github.com/liustack/tifa"
)

// Stats counts the hits, misses and shared renders of the render memo, and the entries and bytes it
// keeps.
func TestStatsCountTheMemo(t *testing.T) {
	e := newSharedEnv(t)
	get(e.s, "/news")
	get(e.s, "/news")
	get(e.s, "/stats")
	get(e.s, "/stats")
	s := e.s.Stats()
	if s.Memo != (tifa.MemoStats{Entries: 2, Hits: 2, Misses: 2}) {
		t.Errorf("render memo %+v", s.Memo)
	}
	if s.MemoBytes <= 0 || s.MemoSize != e.s.memo.size || s.SSR != (tifa.SSRStats{}) {
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
	if _, _, err := runApp(t, context.Background(), app, nil, "--server-addr", "127.0.0.1:0", "--ssr-memo-size", "1MiB"); !errors.Is(err, errStop) {
		t.Fatalf("serve: %v", err)
	}
	if during.MemoSize != 1<<20 {
		t.Errorf("during OnStart: %+v", during)
	}
	if after := tifa.Stats(); after != (tifa.ServerStats{}) {
		t.Errorf("after the stop: %+v", after)
	}
}
