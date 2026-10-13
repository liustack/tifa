//go:build !unix

package devserver

import (
	"errors"
	"os"
	"os/exec"
)

// Run refuses to start on systems without process groups and interrupts, these keep the package building.

func ownGroup(*exec.Cmd) {}

func interruptGroup(*os.Process) error {
	return errors.New("interrupting a process is not supported here")
}

func killGroup(p *os.Process) error { return p.Kill() }
