//go:build unix

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// stopContext is done when this tifa dev is asked to stop: Ctrl-C, SIGTERM (tifa dev stop) or
// SIGHUP, see stopSignals.
func stopContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	ctx, stop := signal.NotifyContext(parent, stopSignals()...)
	return ctx, stop, nil
}

// requestStop asks the tifa dev with the process ID pid to stop, as Ctrl-C does: it sends it
// SIGTERM. release does nothing here.
func requestStop(pid int) (release func(), err error) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil, err
	}
	return func() {}, p.Signal(syscall.SIGTERM)
}
