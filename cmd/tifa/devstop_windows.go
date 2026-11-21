//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"golang.org/x/sys/windows"
)

// Windows has no signal that one program can send another, so tifa dev stop and Ctrl-C during the
// start of --background ask tifa dev to stop through a named event of the session, which tifa dev
// waits on: Local\tifa-dev-stop-<pid>. Its process ID names it, which the lock records. An event
// stays set until its last handle closes, so a request made before tifa dev opened it still
// reaches it, as long as the one who asked holds it.

// stopEvent returns the name of the event that asks the tifa dev with the process ID pid to stop.
func stopEvent(pid int) string { return `Local\tifa-dev-stop-` + strconv.Itoa(pid) }

// openStopEvent creates the stop event of pid, or opens it when it exists.
func openStopEvent(pid int) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(stopEvent(pid))
	if err != nil {
		return 0, err
	}
	// Manual reset: once set it stays set, for whoever waits on it later.
	h, err := windows.CreateEvent(nil, 1, 0, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		err = nil
	}
	if err != nil {
		return 0, fmt.Errorf("the stop event %s: %w", stopEvent(pid), err)
	}
	return h, nil
}

// stopContext is done when this tifa dev is asked to stop: Ctrl-C or Ctrl-Break in its console,
// its console closing (stopSignals), or its stop event, which tifa dev stop sets.
func stopContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	ev, err := openStopEvent(os.Getpid())
	if err != nil {
		return nil, nil, err
	}
	quit, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(ev)
		return nil, nil, err
	}
	ctx, stopSignals := signal.NotifyContext(parent, stopSignals()...)
	ctx, cancel := context.WithCancel(ctx)
	waited := make(chan struct{})
	go func() {
		defer close(waited)
		which, err := windows.WaitForMultipleObjects([]windows.Handle{ev, quit}, false, windows.INFINITE)
		if err == nil && which == windows.WAIT_OBJECT_0 {
			cancel()
		}
	}()
	return ctx, func() {
		windows.SetEvent(quit)
		<-waited
		windows.CloseHandle(quit)
		windows.CloseHandle(ev)
		cancel()
		stopSignals()
	}, nil
}

// requestStop asks the tifa dev with the process ID pid to stop, as Ctrl-C does: it sets its stop
// event. Call release once that tifa dev ended or is given up on: until then the event must stay
// open, as that tifa dev may not have opened it yet.
func requestStop(pid int) (release func(), err error) {
	ev, err := openStopEvent(pid)
	if err != nil {
		return nil, err
	}
	if err := windows.SetEvent(ev); err != nil {
		windows.CloseHandle(ev)
		return nil, err
	}
	return func() { windows.CloseHandle(ev) }, nil
}
