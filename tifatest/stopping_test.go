package tifatest_test

import (
	"context"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/server"
	"github.com/liustack/tifa/tifatest"
)

// cleanups is a testing.TB that keeps its cleanups for the test to run.
type cleanups struct {
	testing.TB
	fns []func()
}

func (c *cleanups) Cleanup(f func()) { c.fns = append(c.fns, f) }

func (c *cleanups) run() {
	for i := len(c.fns) - 1; i >= 0; i-- {
		c.fns[i]()
	}
}

// A request that waits for its context ends when the test does, as on the server: the stop
// cancels it, then runs OnShutdown.
func TestStopCancelsTheRequestsInFlight(t *testing.T) {
	entered := make(chan struct{})
	var shutdown bool
	app := server.App{
		Hooks: server.Hooks{OnShutdown: func(context.Context) error { shutdown = true; return nil }},
		Routes: []server.Route{{ID: "app/wait/route", Path: "/wait", GET: func(c *tifa.Ctx) error {
			close(entered)
			<-c.Context().Done()
			return c.Context().Err()
		}}},
	}
	tb := &cleanups{TB: t}
	srv := tifatest.Start(tb, app)
	go func() {
		if res, err := srv.Client().Get(srv.URL + "/wait"); err == nil {
			res.Body.Close()
		}
	}()
	<-entered
	stopped := make(chan struct{})
	go func() {
		tb.run()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the stop still waits for a request that waits for its context")
	}
	if !shutdown {
		t.Error("OnShutdown did not run")
	}
}
