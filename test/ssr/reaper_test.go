package ssr_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// reaperEnv names the program that this test binary runs as a reaper instead of running tests.
const reaperEnv = "SSR_TEST_REAPER"

func TestMain(m *testing.M) {
	if exe := os.Getenv(reaperEnv); exe != "" {
		os.Exit(reap(exe, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// reap runs the program exe with args, with this process's output, and kills it when its standard
// input ends, which is when the test process that started the reaper ends, however it ends.
// A test that kills its children in t.Cleanup leaves them running when a panic in another
// goroutine or a test timeout ends the process, since neither runs a cleanup. The test process
// ends the input itself to stop the program.
func reap(exe string, args []string) int {
	cmd := exec.Command(exe, args...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, reaperEnv+"=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	go func() {
		io.Copy(io.Discard, os.Stdin)
		cmd.Process.Kill()
	}()
	cmd.Wait()
	return 0
}
