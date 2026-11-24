//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// holdOpen opens the program the way another program that reads it may, such as a virus scanner:
// os.Open shares it for reading and writing but not for deletion, and Windows refuses to rename it
// while it is open.
func holdOpen(t *testing.T, exe string) *os.File {
	t.Helper()
	f, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if err := os.Rename(exe, exe+".probe"); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("renaming a file held open: %v, want a sharing violation: the test proves nothing", err)
	}
	return f
}

// Another program may read the program for a moment right after go build wrote it, and moveAside
// waits for it to let go.
func TestMoveAsideWaitsForAProgramThatReadsTheFile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tifa-dev.exe")
	writeFile(t, exe, "app")
	f := holdOpen(t, exe)
	time.AfterFunc(300*time.Millisecond, func() { f.Close() })
	if err := moveAside(exe); err != nil {
		t.Fatal(err)
	}
	if got := oldExes(t, dir); len(got) != 1 {
		t.Fatalf("moved aside: %q", got)
	}
}

// A file held for longer is an error, after a bounded wait.
func TestMoveAsideGivesUpOnAFileHeldForLong(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tifa-dev.exe")
	writeFile(t, exe, "app")
	holdOpen(t, exe)
	start := time.Now()
	err := moveAside(exe)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("moveAside: %v, want the sharing violation", err)
	}
	if took := time.Since(start); took < heldWait/2 || took > heldWait+time.Second {
		t.Fatalf("moveAside gave up after %s, want about %s", took, heldWait)
	}
}

// The app of tifa dev runs from its program while go build writes the next one. Windows refuses to
// write over a program that runs, and moveAside makes room for it.
func TestMoveAsideMakesRoomWhileTheProgramRuns(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tifa-dev.exe")
	self, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	copyTo, err := os.Create(exe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(copyTo, self); err != nil {
		t.Fatal(err)
	}
	copyTo.Close()
	running := exec.Command(exe, "-test.run=^$")
	running.Env = append(os.Environ(), sleeperEnv+"=1")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { running.Process.Kill(); running.Wait() })

	if err := os.WriteFile(exe, []byte("new"), 0o755); err == nil {
		t.Fatal("Windows let a running program be written over: the test proves nothing")
	}
	if err := moveAside(exe); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("new"), 0o755); err != nil {
		t.Fatalf("writing the new program after moveAside: %v", err)
	}
	// The one that runs cannot be deleted yet and stays, the next call after it ended deletes it.
	if err := moveAside(exe); err != nil {
		t.Fatal(err)
	}
	if got := oldExes(t, dir); len(got) != 2 {
		t.Fatalf("moved aside: %q, want the running one and the new one", got)
	}
	running.Process.Kill()
	running.Wait()
	if err := moveAside(exe); err != nil {
		t.Fatal(err)
	}
	if got := oldExes(t, dir); len(got) != 0 {
		t.Fatalf("left after the program ended: %q", got)
	}
}
