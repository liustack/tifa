//go:build unix

package server

import (
	"fmt"
	"os"
	"syscall"
)

// watchLifeline calls stop when the file descriptor fd reaches the end of its file. tifa dev passes
// the read end of a pipe it holds the write end of and never writes to, so the end comes when
// tifa dev ends, however it ends (see lifeline in internal/devserver). Without this the app would
// run on after a tifa dev that was killed, with its port.
func watchLifeline(fd int, stop func()) error {
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return fmt.Errorf("--lifeline-fd %d: %w", fd, err)
	}
	// What the app starts does not need it.
	syscall.CloseOnExec(fd)
	f := os.NewFile(uintptr(fd), "tifa dev lifeline")
	go func() {
		defer stop()
		var b [1]byte
		for {
			if _, err := f.Read(b[:]); err != nil {
				return // the end of the file, or the pipe broke: tifa dev is gone either way
			}
		}
	}()
	return nil
}
