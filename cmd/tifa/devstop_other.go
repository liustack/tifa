//go:build !unix && !windows

package main

import (
	"context"
	"errors"
	"os/signal"
)

func stopContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	ctx, stop := signal.NotifyContext(parent, stopSignals()...)
	return ctx, stop, nil
}

// requestStop fails here: tifa dev does not run on these systems.
func requestStop(int) (func(), error) { return nil, errors.New("tifa dev does not run on this system") }
