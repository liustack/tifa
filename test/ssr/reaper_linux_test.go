//go:build linux

package ssr_test

import (
	"os/exec"
	"runtime"
	"syscall"
)

// bindToReaper makes Linux kill cmd when the reaper ends, however it ends: cmd is started with a
// signal for the death of its parent. Linux sends it when the thread that started cmd ends, not
// the process, so the reaper's main goroutine stays on its thread until the process ends.
// The returned function is for the reaper to call once cmd has started.
func bindToReaper(cmd *exec.Cmd) (started func(), err error) {
	runtime.LockOSThread()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return func() {}, nil
}
