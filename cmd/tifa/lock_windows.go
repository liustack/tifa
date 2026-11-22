//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset is where the lock sits in .tifa/dev.lock: far past the record. A lock on Windows keeps
// every other handle from reading the bytes it covers, and status reads the record while tifa dev
// holds the lock. Windows allows a lock past the end of a file.
const lockOffset = 1 << 40

// tryLock takes an exclusive lock on f without waiting. It reports false when another open file
// holds it. Like flock on Unix the lock belongs to the open file, and the system drops it when the
// process ends, so no lock outlives its holder.
func tryLock(f *os.File) (bool, error) {
	overlapped := windows.Overlapped{Offset: lockOffset & 0xffffffff, OffsetHigh: lockOffset >> 32}
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

// processAlive reports whether a process with the ID pid runs. A process of another user that
// refuses to be opened runs too.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return true
	}
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}

// addrInUse reports whether a listen failed because another socket holds the address. Windows
// answers WSAEADDRINUSE: syscall.EADDRINUSE is a number Go made up for it, which no call returns.
func addrInUse(err error) bool { return errors.Is(err, windows.WSAEADDRINUSE) }

// portReserved reports whether a listen failed because Windows keeps the port for itself: Hyper-V,
// WSL and Docker reserve ranges of ports (netsh interface ipv4 show excludedportrange
// protocol=tcp), and a listen there gets WSAEACCES.
func portReserved(err error) bool { return errors.Is(err, windows.WSAEACCES) }
