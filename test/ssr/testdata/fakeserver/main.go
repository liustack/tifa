// Command fakeserver stands in for a program built by tifa build in the tests of startProgram: it
// writes its process ID to the file named by FAKE_PIDFILE, says where it listens as the real
// program does, and waits.
package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	if err := os.WriteFile(os.Getenv("FAKE_PIDFILE"), []byte(fmt.Sprint(os.Getpid())), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "listening on http://127.0.0.1:1")
	time.Sleep(time.Hour)
}
