//go:build darwin

package testproc

import (
	"runtime"
	"testing"
)

// deep uses n frames of stack, so that the goroutine that called it has a large stack with little
// of it in use afterwards, which a garbage collection then makes smaller.
//
//go:noinline
func deep(n int) int {
	var pad [256]byte
	if n == 0 {
		return int(pad[0])
	}
	return deep(n-1) + int(pad[n%len(pad)])
}

// openPty names the terminal it opens while the stack of its goroutine moves: the system writes the
// name where the call says, and that must be the buffer the function reads afterwards.
func TestOpenPtyNamesTheTerminalWhileTheStackMoves(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				runtime.GC()
			}
		}
	}()
	for i := range 3000 {
		done := make(chan error)
		go func() {
			deep(200 + i%300)
			master, slave, err := openPty()
			if err == nil {
				slave.Close()
				master.Close()
			}
			done <- err
		}()
		if err := <-done; err != nil {
			t.Fatalf("terminal %d: %v", i, err)
		}
	}
}
