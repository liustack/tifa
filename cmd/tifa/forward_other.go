//go:build !unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// forward runs go tool tifa with args in root and exits with its status. Without exec, this
// process stays as the parent: the console sends Ctrl-C to every process attached to it, so the
// child gets it without help, and this process ignores it to stay alive until the child has
// stopped and to report its status. SIGTERM, which reaches only this process, is passed on. It
// returns only when go tool tifa cannot start.
func forward(root string, args []string) error {
	cmd := exec.Command("go", append([]string{"tool", "tifa"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), forwardedEnv+"=1")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return forwardFailed(root, args, err)
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				if s == syscall.SIGTERM {
					_ = cmd.Process.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	var exit *exec.ExitError
	switch {
	case err == nil:
		os.Exit(0)
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		os.Exit(exit.ExitCode())
	}
	os.Exit(1)
	return nil
}
