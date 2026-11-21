//go:build unix

package bundle

import (
	"errors"
	"syscall"
)

// alive reports whether the process pid exists.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// kill ends the process pid.
func kill(pid int) { syscall.Kill(pid, syscall.SIGKILL) }
