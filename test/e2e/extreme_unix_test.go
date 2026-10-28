//go:build unix

package e2e

import (
	"os/exec"
	"runtime"
	"syscall"
)

// peakRSS returns the most memory the exited process cmd held at once, in bytes.
func peakRSS(cmd *exec.Cmd) (int64, bool) {
	peak := cmd.ProcessState.SysUsage().(*syscall.Rusage).Maxrss
	if runtime.GOOS != "darwin" { // KiB on Linux, bytes on macOS
		peak <<= 10
	}
	return peak, true
}
