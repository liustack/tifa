// Command fakeserver stands in for a program built by tifa build in the tests of startProgram. It
// writes a report to the file named by FAKE_PIDFILE: its process ID, the ID of its parent, and the
// device and inode of each file it holds open from descriptor 3 on, one number or pair a line.
// Then it says where it listens as the real program does, and waits.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

func main() {
	var report strings.Builder
	fmt.Fprintf(&report, "%d\n%d\n", os.Getpid(), os.Getppid())
	for fd := 3; fd < 256; fd++ {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil {
			fmt.Fprintf(&report, "%d:%d\n", uint64(st.Dev), uint64(st.Ino))
		}
	}
	if err := os.WriteFile(os.Getenv("FAKE_PIDFILE"), []byte(report.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "listening on http://127.0.0.1:1")
	time.Sleep(time.Hour)
}
