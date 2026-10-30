//go:build unix

package testproc

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// CloseInheritedOnExec marks each file this process holds open from descriptor 3 on to close when
// the process starts another program. Go does it for the files it opens itself. The ones left are
// those that the process that started this one passed on, such as the lock of a script that runs
// the tests under flock. A program that kept it would hold the lock for as long as it lives.
// Start calls it before its first program. A test binary can call it first thing in TestMain to
// cover the programs it starts without Start too.
func CloseInheritedOnExec() error {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return fmt.Errorf("list the open files: %w", err)
	}
	for _, entry := range entries {
		if fd, err := strconv.Atoi(entry.Name()); err == nil && fd > 2 {
			syscall.CloseOnExec(fd)
		}
	}
	return nil
}
