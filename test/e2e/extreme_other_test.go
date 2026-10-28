//go:build !unix

package e2e

import "os/exec"

func peakRSS(*exec.Cmd) (int64, bool) { return 0, false }
