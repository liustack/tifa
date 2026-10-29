//go:build linux || darwin

package ssr_test

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// closeInheritedFilesOnExec marks each file this process holds open from descriptor 3 on to close
// when the process starts another program. Go does it for the files it opens itself. The ones left
// are those that the process that started this one passed on.
func closeInheritedFilesOnExec() error {
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
