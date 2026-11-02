package server

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
)

// runUntilListening runs a server whose OnStart is onStart, waits for the line about its address
// to show in out, and stops it.
func runUntilListening(t *testing.T, onStart func(), out *syncBuffer) {
	t.Helper()
	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:0"
	s := New(cfg, Hooks{OnStart: func(context.Context) error { onStart(); return nil }})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	until(t, "the listening line", func() bool { return strings.Contains(out.String(), "tifa: listening on http://") })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
}

func textHandler(out *syncBuffer) slog.Handler { return slog.NewTextHandler(out, nil) }

// The startup lines are logged once OnStart has returned, so an app that calls slog.SetDefault
// there gets them in its own handler.
func TestStartupLogsGoToTheDefaultSetInOnStart(t *testing.T) {
	old := slog.Default()
	defer slog.SetDefault(old)
	var before, after syncBuffer
	slog.SetDefault(slog.New(textHandler(&before)))
	runUntilListening(t, func() { slog.SetDefault(slog.New(textHandler(&after))) }, &after)
	if strings.Contains(before.String(), "listening") {
		t.Fatalf("the handler from before OnStart got the startup line:\n%s", before.String())
	}
	if !strings.Contains(after.String(), "level=INFO msg=\"tifa: listening on http://127.0.0.1:") {
		t.Fatalf("the handler set in OnStart got:\n%s", after.String())
	}
}

// logDefault makes out the destination of slog.Default and returns the function that restores it.
func logDefault(out *syncBuffer) (restore func()) {
	old := slog.Default()
	slog.SetDefault(slog.New(textHandler(out)))
	return func() { slog.SetDefault(old) }
}
