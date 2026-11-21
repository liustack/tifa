//go:build !unix

package devserver

import (
	"os"
	"os/exec"
)

// Run refuses to start on systems without process groups and interrupts, these keep the package building.

func ownGroup(*exec.Cmd) {}

func interruptGroup(p *os.Process) error { return p.Kill() } // probe hack

func killGroup(p *os.Process) error { return p.Kill() }

// lifeline needs file descriptors to pass on, which Run does not run without.
type lifeline struct{}

func newLifeline(*exec.Cmd) (*lifeline, error) { return &lifeline{}, nil }

func (*lifeline) started() {}

func (*lifeline) close() {}
