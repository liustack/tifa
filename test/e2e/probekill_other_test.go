//go:build !unix

package e2e

import "os"

func probeAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}

func probeKill(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}
