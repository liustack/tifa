package e2e

import (
	"os"
	"os/exec"
	"strconv"
)

// killWithGroup keeps the default: the deadline kills the command itself.
func killWithGroup(cmd *exec.Cmd) {}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}

// endProcessTree kills the process pid and everything below it.
func endProcessTree(pid int) {
	exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}
