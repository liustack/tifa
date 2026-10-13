//go:build unix

package devserver

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ownGroup puts the app in a process group of its own, so stopping it reaches every process it started.
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func interruptGroup(p *os.Process) error { return signalGroup(p, syscall.SIGINT) }

func killGroup(p *os.Process) error { return signalGroup(p, syscall.SIGKILL) }

// signalGroup signals the process group led by p. It returns os.ErrProcessDone when the group is empty.
func signalGroup(p *os.Process, sig syscall.Signal) error {
	err := syscall.Kill(-p.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
