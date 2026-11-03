//go:build !unix

package main

import "os"

// tryLock does nothing here: tifa dev does not run on these systems, and a tifa build that starts
// while another one runs is not caught.
func tryLock(*os.File) (bool, error) { return true, nil }

// processAlive reports true for any process ID: tifa dev does not run on these systems, and the
// answer of its address tells whether one runs.
func processAlive(pid int) bool { return pid > 0 }
