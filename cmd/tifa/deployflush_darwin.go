//go:build darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// fread is FREAD of <sys/fcntl.h>: TIOCFLUSH drops the input queue for it.
const fread = 0x1

// flushInput throws away the keys the terminal holds and nobody read. Setup can take seconds to plan,
// and a developer who presses Enter meanwhile must not answer the question that comes after. It
// does nothing when f is not a terminal.
func flushInput(f *os.File) {
	_ = unix.IoctlSetPointerInt(int(f.Fd()), unix.TIOCFLUSH, fread)
}
