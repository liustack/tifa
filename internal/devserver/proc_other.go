//go:build !unix && !windows

package devserver

import (
	"errors"
	"os"
	"os/exec"
)

// supported says whether Run runs on this system: not without process groups and a way to ask the
// app to stop. What follows keeps the package building.
const supported = false

func ownGroup(*exec.Cmd) {}

type group struct{}

func joinGroup(*os.Process) (*group, error) { return &group{}, nil }

func (*group) close() {}

func interruptGroup(*app) error {
	return errors.New("interrupting a process is not supported here")
}

func killGroup(a *app) error { return a.cmd.Process.Kill() }

// lifeline needs file descriptors to pass on, which Run does not run without.
type lifeline struct{}

func newLifeline(*exec.Cmd) (*lifeline, error) { return &lifeline{}, nil }

func (*lifeline) started() {}

func (*lifeline) close() {}

func BindChildren() error { return nil }
