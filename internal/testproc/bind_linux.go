//go:build linux

package testproc

import (
	"os/exec"
	"runtime"
	"sync"
	"syscall"
)

// start makes Linux send cmd a SIGKILL when this process ends. Linux sends it when the thread that
// started cmd ends, not the process, so cmd is started from a thread that lives as long as the
// process does.
func start(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
	var err error
	onStarterThread(func() { err = cmd.Start() })
	return err
}

var starter struct {
	once sync.Once
	jobs chan func()
}

// onStarterThread runs f on the one thread that never ends. A goroutine that locks its thread and
// does not return keeps it for as long as the process lives.
func onStarterThread(f func()) {
	starter.once.Do(func() {
		starter.jobs = make(chan func())
		go func() {
			runtime.LockOSThread()
			for job := range starter.jobs {
				job()
			}
		}()
	})
	done := make(chan struct{})
	starter.jobs <- func() {
		defer close(done)
		f()
	}
	<-done
}
