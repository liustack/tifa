//go:build unix

package devserver

import "syscall"

// raiseFileLimit lifts the soft limit on open files to the hard limit and reports whether it rose.
// The Go runtime does the same at start, this catches a limit lowered since.
func raiseFileLimit() bool {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil || lim.Cur >= lim.Max {
		return false
	}
	lim.Cur = lim.Max
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim) == nil
}

// fileLimit returns the soft limit on open files, 0 when unknown.
func fileLimit() uint64 {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return 0
	}
	return lim.Cur
}
