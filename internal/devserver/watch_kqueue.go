//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package devserver

import (
	"errors"
	"math"
	"os"
	"syscall"
)

// kqueue keeps a file descriptor open for a watched directory and for every entry in it.

// entryCost is how many file descriptors an entry that appears in a watched directory takes.
const entryCost = 1

// watchCost returns how many file descriptors watching dir takes.
func watchCost(dir string) int {
	f, err := os.Open(dir)
	if err != nil {
		return 1 // adding the watch reports the error
	}
	defer f.Close()
	names, _ := f.Readdirnames(-1)
	return 1 + len(names)
}

// watchBudget returns how many file descriptors the watches may take: half of what the process
// may open, so tifa dev keeps enough for its own work and leaves the system table alone.
func watchBudget() int {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return math.MaxInt
	}
	n := uint64(lim.Cur) // int64 on some systems
	// macOS takes infinity for the limit but still stops at kern.maxfilesperproc.
	if max, err := syscall.SysctlUint32("kern.maxfilesperproc"); err == nil && uint64(max) < n {
		n = uint64(max)
	}
	if n > math.MaxInt32 {
		return math.MaxInt
	}
	return int(n) / 2
}

// outOfWatches reports whether err means the process or the system has no file descriptor left.
func outOfWatches(err error) bool {
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE)
}
