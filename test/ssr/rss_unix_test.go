//go:build unix

package ssr_test

import (
	"fmt"
	"syscall"
	"testing"
)

// peakRSS returns the most memory the test process held at once.
func peakRSS(t *testing.T) string {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%.1f MiB", float64(usage.Maxrss*maxrssUnit)/(1<<20))
}
