//go:build windows

package e2e

import (
	"errors"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
)

// interruptDev asks the tifa dev of p to stop as Ctrl-C in its console does. A test cannot send
// Ctrl-C to one program alone on Windows, so it sets the event that tifa dev stop sets and that
// tifa dev handles as it handles Ctrl-C: Local\tifa-dev-stop-<pid>, see devstop_windows.go in
// cmd/tifa. Call release once tifa dev ended: a tifa dev that has not opened the event yet finds it
// set only while a handle to it is open.
func interruptDev(p *os.Process) (release func(), err error) {
	name, err := windows.UTF16PtrFromString(`Local\tifa-dev-stop-` + strconv.Itoa(p.Pid))
	if err != nil {
		return nil, err
	}
	ev, err := windows.CreateEvent(nil, 1, 0, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if err := windows.SetEvent(ev); err != nil {
		windows.CloseHandle(ev)
		return nil, err
	}
	return func() { windows.CloseHandle(ev) }, nil
}
