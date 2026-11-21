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
