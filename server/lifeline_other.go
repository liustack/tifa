//go:build !unix

package server

import "errors"

// watchLifeline fails: file descriptors to pass on are a Unix thing, and tifa dev does not run
// elsewhere.
func watchLifeline(int, func()) error {
	return errors.New("--lifeline-fd is not supported on this system")
}
