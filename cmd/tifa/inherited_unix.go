//go:build unix

package main

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// closeInheritedOnExec marks each file the process holds open from descriptor 3 on to close when it
// starts another program. Go does it for the files it opens itself. The ones left came with the
// process, such as the lock of a script that runs tifa dev under flock. The app and the driver of
// Vite would keep it for as long as they live, which can be longer than tifa dev.
func closeInheritedOnExec() error {
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
