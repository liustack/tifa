//go:build unix

package main

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive lock on f without waiting. It reports false when another open file
// holds it. The system drops the lock when the process ends, so no lock outlives its holder.
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// processAlive reports whether a process with the ID pid runs. A process of another user that
// refuses the signal runs too.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
