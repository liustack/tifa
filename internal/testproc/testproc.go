// Package testproc starts the programs that tests run in the background, such as the program
// tifa build makes or a Node server, so that the system ends them when the test process ends,
// however it ends. t.Cleanup does not run when a panic in another goroutine, a test timeout or a
// SIGKILL ends the test process, so a program stopped there alone is left running with the init
// process as its parent, and holds its port, its memory and every file it inherited.
//
// It is for tests only. The framework does not use it.
package testproc

import (
	"os/exec"
	"sync"
)

// Start starts cmd as cmd.Start does, so that the system ends it when this process ends, however
// it ends. Linux sends the program a SIGKILL, and macOS, which has no such signal, hangs up the
// terminal the program runs on, which stops a program that does not ignore SIGHUP. Only the
// program is bound, not what it starts in turn: on macOS what stays in its process group stops
// with it, and on Linux nothing does. The program also does not hold the files this process
// inherited (see CloseInheritedOnExec).
//
// The program is bound to the process, not to the goroutine or the test that started it, so a
// test still stops the programs it started when it ends, as it did before, with Kill and Wait.
func Start(cmd *exec.Cmd) error {
	inherited.Do(func() { inheritedErr = CloseInheritedOnExec() })
	if inheritedErr != nil {
		return inheritedErr
	}
	return start(cmd)
}

var (
	inherited    sync.Once
	inheritedErr error
)
