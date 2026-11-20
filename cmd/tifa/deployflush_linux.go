//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// flushInput throws away the keys the terminal holds and nobody read. Setup can take seconds to plan,
// and a developer who presses Enter meanwhile must not answer the question that comes after. It
// does nothing when f is not a terminal.
func flushInput(f *os.File) {
	_ = unix.IoctlSetInt(int(f.Fd()), unix.TCFLSH, unix.TCIFLUSH)
}
