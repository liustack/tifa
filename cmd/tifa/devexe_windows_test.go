//go:build windows

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
