//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach makes the program c starts leave the terminal: it gets a session of its own, so a closed
// terminal (SIGHUP) and Ctrl-C in it do not reach it.
func detach(c *exec.Cmd) error {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return nil
}
