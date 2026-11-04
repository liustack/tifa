package tifatest_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/server"
	"github.com/liustack/tifa/tifatest"
)

// stopErrors is cleanups that also keeps what the cleanups report with Errorf.
type stopErrors struct {
	cleanups
	errs []string
}

func (s *stopErrors) Errorf(format string, args ...any) {
	s.errs = append(s.errs, fmt.Sprintf(format, args...))
}

// A handler that ignores the end of its request and outlives server.shutdown_timeout keeps the
// app in the process: its test fails, and a later Start fails and says why, until the handler
// returns.
func TestAHandlerThatOutlivesTheStopHoldsTheApp(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	app := server.App{
		Settings: map[string]string{"server.shutdown_timeout": "100ms"},
		Routes: []server.Route{{ID: "app/stuck/route", Path: "/stuck", GET: func(*tifa.Ctx) error {
			close(entered)
			<-release
			return nil
		}}},
	}
	tb := &stopErrors{cleanups: cleanups{TB: t}}
	srv := tifatest.Start(tb, app)
	go func() {
		if res, err := srv.Client().Get(srv.URL + "/stuck"); err == nil {
			res.Body.Close()
		}
	}()
	<-entered
	tb.run()
	if msg := strings.Join(tb.errs, "\n"); !strings.Contains(msg, "handlers still running") {
		t.Errorf("the stop of a test whose handler still runs reported %q", msg)
	}

	plain := signInApp()
	plain.Hooks.OnStart, plain.Hooks.OnShutdown = nil, nil
	f := &fatal{TB: t}
	if srv := tifatest.Start(f, plain); srv != nil || !strings.Contains(f.msg, "a handler of an earlier test") {
		close(release)
		t.Fatalf("Start while a handler of an earlier test runs: %v, %q", srv, f.msg)
	}

	// Once the handler returns, the app is free again.
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		f := &fatal{TB: t}
		if srv := tifatest.Start(f, plain); srv != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Start after the handler returned: %q", f.msg)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
