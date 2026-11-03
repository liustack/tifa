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
