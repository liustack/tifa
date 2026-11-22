//go:build !windows

package devserver

import "os"

// openToHash opens the file name for reading while the watcher hashes it.
func openToHash(name string) (*os.File, error) { return os.Open(name) }
