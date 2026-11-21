//go:build windows

package server

import (
	"os"
	"testing"
)

// pipeEnds makes the read end of a new pipe the standard input, as the app has it from tifa dev
// on Windows, and returns its number, 0, and the write end tifa dev holds.
func pipeEnds(t *testing.T) (int, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = stdin
		w.Close()
		r.Close()
	})
	return 0, w
}

// On Windows the lifeline is the standard input, and no other number.
func TestDevMainRefusesALifelineThatIsNotStdin(t *testing.T) {
	err := watchLifeline(3, func() {})
	if err == nil {
		t.Fatal("a lifeline of fd 3 was accepted")
	}
}
