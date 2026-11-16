//go:build !unix

package main

import (
	"errors"
	"os/exec"
)

// detach fails here: tifa dev does not run on these systems.
func detach(*exec.Cmd) error {
	return errors.New("tifa dev does not run on this system yet, use WSL")
}
