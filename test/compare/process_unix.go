//go:build unix

package compare

import "syscall"

// raiseFileLimit lets the process open as many files as it may: the open model keeps thousands of
// connections open at high rates.
func raiseFileLimit() {
	var lim syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim) == nil && lim.Cur < lim.Max {
		lim.Cur = lim.Max
		if lim.Cur > 1<<20 {
			lim.Cur = 1 << 20
		}
		syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
	}
}

// selfCPU is the processor time this process has used, in seconds.
func selfCPU() float64 {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6
}
