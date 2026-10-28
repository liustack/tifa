//go:build unix

package ssr_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestStartProgramHelper is the test process of TestStartProgramEndsWithTheTestProcess: it starts
// the fake program and then dies from a panic in a goroutine of its own, which runs no t.Cleanup.
func TestStartProgramHelper(t *testing.T) {
	exe := os.Getenv("FAKE_SERVER")
	if exe == "" {
		t.Skip("runs only as the helper of TestStartProgramEndsWithTheTestProcess")
	}
	startProgram(t, exe)
	go panic("a panic outside the test goroutine")
	time.Sleep(time.Minute)
}

// A program that startProgram runs must not outlive the test process when that process dies without
// running its cleanups: a panic in another goroutine, or a timeout, ends the process at once.
func TestStartProgramEndsWithTheTestProcess(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "fakeserver")
	if out, err := exec.Command("go", "build", "-o", exe, "./testdata/fakeserver").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	pidFile := filepath.Join(dir, "pid")
	helper := exec.Command(os.Args[0], "-test.run=^TestStartProgramHelper$")
	helper.Env = append(os.Environ(), "FAKE_SERVER="+exe, "FAKE_PIDFILE="+pidFile)
	if out, err := helper.CombinedOutput(); err == nil || !strings.Contains(string(out), "a panic outside the test goroutine") {
		t.Fatalf("the helper should die from its panic: %v\n%s", err, out)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	// Whatever the result, do not leave the fake program running.
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
	}
	t.Fatalf("the program (pid %d) still runs after its test process died", pid)
}

// When the test ends normally, its cleanup stops the program before the test returns.
func TestStartProgramStopsAtCleanup(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "fakeserver")
	if out, err := exec.Command("go", "build", "-o", exe, "./testdata/fakeserver").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	pidFile := filepath.Join(dir, "pid")
	t.Setenv("FAKE_PIDFILE", pidFile)
	t.Run("start", func(t *testing.T) { startProgram(t, exe) })
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the program (pid %d) still runs after its test ended", pid)
	}
}
