//go:build !unix

package main

import "os"

// tryLock does nothing here: tifa dev does not run on these systems, and a tifa build that starts
// while another one runs is not caught.
func tryLock(*os.File) (bool, error) { return true, nil }
