//go:build !unix

package ssr

import "testing"

// peakRSS reports that the peak memory is not measured: the system has no getrusage.
func peakRSS(*testing.T) string { return "not measured" }
