//go:build !linux && !darwin

package testproc

import "os/exec"

// start only starts cmd: other systems have no way here to end it with this process.
func start(cmd *exec.Cmd) error { return cmd.Start() }
