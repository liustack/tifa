//go:build unix

package server

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// pipeEnds returns the number of a file descriptor that reads the pipe, as the app inherits it
// from tifa dev, and the write end tifa dev holds.
func pipeEnds(t *testing.T) (int, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// A copy of the descriptor, so that the app takes over a number no File of the test owns.
	fd, err := syscall.Dup(int(r.Fd()))
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return fd, w
}

// The app of tifa dev stops, as it does on SIGTERM, when tifa dev ends: the write end of its
// lifeline closes, however tifa dev ended.
func TestDevMainStopsWhenTheLifelineEnds(t *testing.T) {
	fd, w := pipeEnds(t)
	started := make(chan struct{})
	var events []string
	app := App{Hooks: Hooks{
		OnStart: func() error {
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

// A lifeline that is not an open file descriptor is a mistake of whoever started the app: it fails
// at once rather than running without one.
func TestDevMainRefusesALifelineThatIsNotOpen(t *testing.T) {
	fd, _ := pipeEnds(t)
	if err := syscall.Close(fd); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run(context.Background(), App{}, []string{"--server-addr", freeAddr(t), "--lifeline-fd", strconv.Itoa(fd)}, nil, &out, &out, reloader)
	if err == nil || !strings.Contains(err.Error(), "--lifeline-fd") || !strings.Contains(err.Error(), "bad file descriptor") {
		t.Fatalf("run with a closed lifeline returned %v", err)
	}
}

// Only the main of tifa dev has the flag, a program of tifa build does not.
func TestMainHasNoLifelineFlag(t *testing.T) {
	_, _, err := runApp(t, context.Background(), App{}, nil, "--lifeline-fd", "3")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("Main accepted --lifeline-fd: %v", err)
	}
}
