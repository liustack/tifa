//go:build windows

package server

import (
	"fmt"
	"os"
)

// watchLifeline calls stop when the standard input reaches its end. On Windows tifa dev passes the
// read end of a pipe as the standard input of the app, fd 0, since Windows passes a program no other
// files by number. The end comes when tifa dev ends, however it ends, and when tifa dev closes the
// pipe to stop the app: Windows has no signal one program can send another (see lifeline in
// internal/devserver).
func watchLifeline(fd int, stop func()) error {
	if fd != 0 {
		return fmt.Errorf("--lifeline-fd %d: on Windows the lifeline is the standard input, 0", fd)
	}
	in := os.Stdin
	go func() {
		defer stop()
		var b [1]byte
		for {
			if _, err := in.Read(b[:]); err != nil {
				return // the end of the file, or the pipe broke: tifa dev is gone or wants the app to stop
			}
		}
	}()
	return nil
}
