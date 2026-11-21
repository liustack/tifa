//go:build windows

package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach makes the program c starts leave the console: it gets a console of its own with no window
// (CREATE_NO_WINDOW), which the programs it starts, Node, go and the app, share, so none of them
// opens a window, and Ctrl-C or closing the console it was started from do not reach it.
// DETACHED_PROCESS would leave it without a console, and each program it starts would open one,
// a window that flashes up. It is stopped through its stop event, see requestStop.
func detach(c *exec.Cmd) error {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW}
	return nil
}
