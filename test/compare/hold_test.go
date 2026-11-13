package compare

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A machine can have a command of its own that keeps it quiet for a timing round (it queues the
// round with the other work and takes the locks). holdWith runs that command around a round: the
// command runs what it is given once the machine is quiet, and the round ends it.
func TestHoldWithRunsTheCommandOfTheMachineAroundARound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "quiet")
	script := filepath.Join(dir, "quiet.sh")
	// As ~/bin/quiet does: say that a round runs, run the command, and take the word back at the end.
	if err := os.WriteFile(script, []byte("#!/bin/sh\n: > \""+marker+"\"\ntrap 'rm -f \""+marker+"\"' EXIT INT TERM\n\"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	release, err := holdWith(context.Background(), []string{script})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the command of the machine does not run while the round does: %v", err)
	}
	release()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("the command of the machine still runs after the round: %v", err)
	}

	// A command that ends before the machine is quiet is an error, with what it wrote.
	failing := filepath.Join(dir, "no.sh")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\necho no room >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := holdWith(context.Background(), []string{failing}); err == nil || !strings.Contains(err.Error(), "no room") {
		t.Fatalf("got %v, want the failure of the command", err)
	}

	// A wait that its context ends gives up, and leaves nothing running.
	slow := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\n: > \""+marker+"\"\ntrap 'rm -f \""+marker+"\"; exit 1' EXIT INT TERM\nsleep 30 &\nwait $!\n\"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := holdWith(ctx, []string{slow}); err == nil {
		t.Fatal("no error when the context ended")
	}
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the command of the machine still runs after the wait was given up")
}

// No command: nothing to run.
func TestHoldWithNoCommand(t *testing.T) {
	release, err := holdWith(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
