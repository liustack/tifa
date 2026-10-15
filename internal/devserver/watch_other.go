//go:build !(darwin || dragonfly || freebsd || netbsd || openbsd || linux)

package devserver

import "math"

func watchBudget() int { return math.MaxInt }

// outOfWatches knows no error for running out of watches on these systems.
func outOfWatches(err error) bool { return false }
