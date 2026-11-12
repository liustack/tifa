package compare

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// A run that shares its machine holds the lock files only while a server of it runs: holdLocks
// takes them in the order given, waits for one that another process holds, and gives them all back.
func TestHoldLocksTakesTheFilesInOrderAndGivesThemBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock")
	}
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.lock"), filepath.Join(dir, "b.lock")
	held := func(path string) bool {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			return true
		}
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return false
	}
	release, err := holdLocks(context.Background(), []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if !held(a) || !held(b) {
		t.Fatal("the locks are not held")
	}
	release()
	if held(a) || held(b) {
		t.Fatal("the locks were not given back")
	}

	// Another holder of the second file: the first is taken and kept, and the call waits.
	other, err := os.OpenFile(b, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := syscall.Flock(int(other.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan func(), 1)
	go func() {
		r, err := holdLocks(context.Background(), []string{a, b})
		if err != nil {
			t.Error(err)
		}
		done <- r
	}()
	select {
	case <-done:
		t.Fatal("took a lock another process holds")
	case <-time.After(300 * time.Millisecond):
	}
	if !held(a) {
		t.Fatal("the first lock is not kept while waiting for the second")
	}
	syscall.Flock(int(other.Fd()), syscall.LOCK_UN)
	select {
	case r := <-done:
		r()
	case <-time.After(5 * time.Second):
		t.Fatal("did not take the lock after it was given back")
	}

	// A wait ends with its context, and keeps nothing.
	syscall.Flock(int(other.Fd()), syscall.LOCK_EX)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := holdLocks(ctx, []string{a, b}); err == nil {
		t.Fatal("no error when the context ended")
	}
	if held(a) {
		t.Fatal("the first lock is kept after the wait was given up")
	}
}

// No lock files: nothing to take, and a release that does nothing.
func TestHoldLocksWithNoFiles(t *testing.T) {
	release, err := holdLocks(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

// A run on a machine with an announce folder says that it waits for or runs a round by a file
// named by its pid there, so that others start no new work in front of it, and takes the file
// away when the round is over.
func TestAnnounceLeavesAFileNamedByThePidWhileARoundRuns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "timing.d")
	done, err := announce(dir)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, strconv.Itoa(os.Getpid()))
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("no file of the run: %v", err)
	}
	// Two servers of a run never overlap, but a second announce must not take the file of the first.
	again, err := announce(dir)
	if err != nil {
		t.Fatal(err)
	}
	again()
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the file went while the first round still runs: %v", err)
	}
	done()
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("the file is still there after the round: %v", err)
	}
	// No folder: nothing to say.
	none, err := announce("")
	if err != nil {
		t.Fatal(err)
	}
	none()
}
