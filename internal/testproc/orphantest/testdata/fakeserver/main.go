// Command fakeserver stands in for a program built by tifa build or a Node server in the tests of
// internal/testproc. It writes a report to the file named by FAKE_PIDFILE: its process ID, the ID
// of its parent, and the device and inode of each file it holds open from descriptor 3 on, one
// number or pair a line. Then it listens on the address after --server-addr (default
// 127.0.0.1:0), says where as the real program does, and answers every request with 200.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
)

func main() {
	addr := "127.0.0.1:0"
	for i, arg := range os.Args {
		if arg == "--server-addr" && i+1 < len(os.Args) {
			addr = os.Args[i+1]
		}
	}
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
	l, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "listening on http://%s\n", l.Addr())
	err = http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") }))
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
