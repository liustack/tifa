//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// forward replaces this process with go tool tifa and args, run in root, so the forwarded command
// has the pid, the streams, the signals and the exit status of tifa itself: a SIGINT sent to tifa
// alone reaches it as surely as a Ctrl-C sent to the process group. It returns only when go tool
// tifa cannot start.
func forward(root string, args []string) error {
	gocmd, err := exec.LookPath("go")
	if err != nil {
		return forwardFailed(root, args, err)
	}
	if err := os.Chdir(root); err != nil {
		return forwardFailed(root, args, err)
	}
	argv := append([]string{"go", "tool", "tifa"}, args...)
	return forwardFailed(root, args, syscall.Exec(gocmd, argv, append(os.Environ(), forwardedEnv+"=1")))
}
