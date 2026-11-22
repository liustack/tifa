//go:build unix

package e2e

import (
	"os"
	"syscall"
)

// interruptDev asks the tifa dev of p to stop as Ctrl-C in its terminal does: it sends SIGINT.
// release does nothing here.
func interruptDev(p *os.Process) (release func(), err error) {
	return func() {}, p.Signal(syscall.SIGINT)
}
