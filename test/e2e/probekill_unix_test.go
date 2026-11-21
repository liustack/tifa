//go:build unix

package e2e

import (
	"errors"
	"syscall"
)

func probeAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func probeKill(pid int) { syscall.Kill(pid, syscall.SIGKILL) }
