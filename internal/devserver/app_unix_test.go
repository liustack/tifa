//go:build unix

package devserver

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitForPID reads the first line the app printed as a process ID.
func waitForPID(t *testing.T, out *syncBuffer) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if line, _, ok := strings.Cut(out.String(), "\n"); ok {
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil {
				t.Fatalf("first line %q is not a PID", line)
			}
			return pid
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the app printed no PID within 5s")
	return 0
}

func gone(pid int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func stopWithin(t *testing.T, a *app, timeout, limit time.Duration) error {
	t.Helper()
	stopped := make(chan error, 1)
	go func() { stopped <- a.stop(timeout) }()
	select {
	case err := <-stopped:
		return err
	case <-time.After(limit):
		t.Fatalf("stop still blocked after %s", limit)
		return nil
	}
}

func TestStopKillsTheAppsChildren(t *testing.T) {
	var out syncBuffer
	// The shell ignores the interrupt, and so does its background child.
	a, err := startApp([]string{"/bin/sh", "-c", "trap '' INT; sleep 60 & echo $!; wait"}, t.TempDir(), "127.0.0.1:1", &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	child := waitForPID(t, &out)
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if err := stopWithin(t, a, 50*time.Millisecond, 5*time.Second); err == nil || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("got %v, want an error saying the app was killed", err)
	}
	if !gone(child) {
		t.Fatalf("child %d still runs after the app was killed", child)
	}
}

func TestStopDoesNotWaitForADetachedChildsOutput(t *testing.T) {
	t.Setenv("TIFA_FAKE_APP_DETACHED_CHILD", "1")
	var out syncBuffer
	a, addr := startFakeApp(t, &out)
	child := waitForPID(t, &out)
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if err := a.waitReady(t.Context(), addr, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	// The child left the process group and keeps the output pipe open after the app exits.
	if err := stopWithin(t, a, 5*time.Second, appWaitDelay+3*time.Second); err != nil {
		t.Fatal(err)
	}
}

// startDetachedChild starts a process in its own process group that writes to our stdout.
func startDetachedChild() error {
	cmd := exec.Command("/bin/sleep", "60")
	cmd.Stdout = os.Stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, err := os.Stdout.WriteString(strconv.Itoa(cmd.Process.Pid) + "\n")
	return err
}
