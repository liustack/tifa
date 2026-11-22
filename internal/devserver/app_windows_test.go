//go:build windows

package devserver

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// gone reports whether the process pid ends within 5 seconds.
func gone(pid int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			return true // no process has the number any more
		}
		ev, err := windows.WaitForSingleObject(h, 0)
		windows.CloseHandle(h)
		if err == nil && ev == windows.WAIT_OBJECT_0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// The job of the app holds what the app started, and killing it ends them all.
func TestStopKillsTheAppsChildren(t *testing.T) {
	t.Setenv("TIFA_FAKE_APP_ON_INTERRUPT", "ignore")
	t.Setenv("TIFA_FAKE_APP_CHILD", "1")
	var out syncBuffer
	a, addr := startFakeApp(t, &out)
	child := waitForPID(t, &out)
	if err := a.waitReady(t.Context(), addr, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := stopWithin(t, a, 50*time.Millisecond, 5*time.Second); err == nil || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("got %v, want an error saying the app was killed", err)
	}
	if !gone(child) {
		t.Fatalf("child %d still runs after the app was killed", child)
	}
}

// An app that stops when asked leaves no child behind either: the job ends them once it exited, as
// the group does on Unix.
func TestStopEndsTheChildrenOfAnAppThatExited(t *testing.T) {
	t.Setenv("TIFA_FAKE_APP_CHILD", "1")
	var out syncBuffer
	a, addr := startFakeApp(t, &out)
	child := waitForPID(t, &out)
	if err := a.waitReady(t.Context(), addr, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := stopWithin(t, a, 5*time.Second, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "shutdown done") {
		t.Fatalf("the app did not shut down cleanly, output %q", out.String())
	}
	if !gone(child) {
		t.Fatalf("child %d still runs after the app stopped", child)
	}
}

// What tifa dev starts after BindChildren ends when tifa dev is killed, which runs no code of it.
func TestBindChildrenEndsThemWithTheProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var out syncBuffer
	dev := exec.Command(exe)
	dev.Env = append(os.Environ(), "TIFA_FAKE_BOUND=1")
	dev.Stdout = &out
	if err := dev.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dev.Process.Kill(); dev.Wait() })
	child := waitForPID(t, &out)
	if err := dev.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if !gone(child) {
		t.Fatalf("child %d still runs after tifa dev was killed", child)
	}
}
