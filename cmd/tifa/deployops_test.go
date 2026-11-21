package main

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a buffer that a command writes to while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// logs prints the journal of the user units of the app, as the user tifa, the last 100 lines
// unless -n says otherwise.
func TestDeployLogs(t *testing.T) {
	root, _, calls := envApp(t, "200")
	out, _, err := run(t, "deploy", "logs", "--dir", root)
	if err != nil || !strings.Contains(out, "the app said goodbye") {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "journalctl --user -u tifa-shop.service -u tifa-shop.socket -n 100 --no-pager\n"
	if !strings.Contains(calls(), "ssh tifa vps\n"+want) {
		t.Errorf("calls:\n%s", calls())
	}
	if _, _, err := run(t, "deploy", "logs", "-n", "5", "--dir", root); err != nil || !strings.Contains(calls(), " -n 5 --no-pager\n") {
		t.Errorf("-n 5: %v\n%s", err, calls())
	}
	if _, _, err := run(t, "deploy", "logs", "-n", "-1", "--dir", root); err == nil {
		t.Error("-n -1: no error")
	}
}

// With -f the journal follows until Ctrl-C, which ends the command cleanly.
func TestDeployLogsFollowEndsOnCtrlC(t *testing.T) {
	root, _, calls := envApp(t, "200")
	var out syncBuffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"deploy", "logs", "-f", "--dir", root})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(out.String(), "following"); {
		if time.Now().After(deadline) {
			t.Fatalf("journalctl -f did not start:\n%s\n%s", out.String(), calls())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(calls(), " --no-pager -f\n") {
		t.Errorf("calls:\n%s", calls())
	}
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := self.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Ctrl-C: %v\n%s", err, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("logs -f did not end on Ctrl-C")
	}
}
