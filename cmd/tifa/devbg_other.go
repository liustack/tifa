//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// probe hack: detached process with its own group
func detach(c *exec.Cmd) error {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000208}
	return nil
}
