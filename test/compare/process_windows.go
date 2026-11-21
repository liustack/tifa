//go:build windows

package compare

import "syscall"

// raiseFileLimit does nothing: Windows has no limit on open files per process that needs raising.
func raiseFileLimit() {}

// selfCPU is the processor time this process has used, in seconds.
func selfCPU() float64 {
	var created, exited, kernel, user syscall.Filetime
	if syscall.GetProcessTimes(syscall.Handle(^uintptr(0)), &created, &exited, &kernel, &user) != nil {
		return 0
	}
	// A Filetime counts 100 ns.
	ticks := func(f syscall.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
	return float64(ticks(kernel)+ticks(user)) / 1e7
}
