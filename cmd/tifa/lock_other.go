//go:build !unix && !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// tryLock does nothing here: tifa dev does not run on these systems, and a tifa build that starts
// while another one runs is not caught.
func tryLock(*os.File) (bool, error) { return true, nil }

// processAlive reports true for any process ID: tifa dev does not run on these systems, and the
// answer of its address tells whether one runs.
func processAlive(pid int) bool { return pid > 0 }

func addrInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }

func portReserved(error) bool { return false }
