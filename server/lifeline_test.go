//go:build unix || windows

package server

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The app of tifa dev stops, as it does on SIGTERM, when tifa dev ends: the write end of its
// lifeline closes, however tifa dev ended.
func TestDevMainStopsWhenTheLifelineEnds(t *testing.T) {
	fd, w := pipeEnds(t)
	started := make(chan struct{})
	var events []string
	app := App{Hooks: Hooks{
		OnStart: func(context.Context) error {
			close(started)
			return nil
		},
		OnShutdown: func(context.Context) error {
			events = append(events, "shutdown")
			return nil
		},
	}}
	done := make(chan error, 1)
	go func() {
		var out bytes.Buffer
		args := []string{"--server-addr", freeAddr(t), "--lifeline-fd", strconv.Itoa(fd)}
		done <- run(context.Background(), app, args, nil, &out, &out, reloader)
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("the app stopped before it started: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the app did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("the app stopped while its lifeline was held: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	w.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the app stopped with %v, want a clean stop", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the app kept running after its lifeline ended")
	}
	if strings.Join(events, ",") != "shutdown" {
		t.Fatalf("events %q, want OnShutdown to have run", events)
	}
}

// Only the main of tifa dev has the flag, a program of tifa build does not.
func TestMainHasNoLifelineFlag(t *testing.T) {
	out, _, err := runApp(t, context.Background(), App{}, nil, "serve", "--help")
	if err != nil || !strings.Contains(out, "--dist") || strings.Contains(out, "lifeline") {
		t.Fatalf("Main has --lifeline-fd: %v\n%s", err, out)
	}
}
