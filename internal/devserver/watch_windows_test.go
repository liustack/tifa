//go:build windows

package devserver

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// holdPage starts a watcher of dir whose first hash of app/page.tsx after arm finds the file held
// by another program, which lets no one else open it. hits receives each hash that found it held,
// and release lets the file go.
func holdPage(t *testing.T, dir string) (w *watcher, arm func(), hits <-chan struct{}, release func()) {
	t.Helper()
	page := filepath.Join(dir, "app", "page.tsx")
	var armed atomic.Bool
	handle := make(chan windows.Handle, 1)
	hit := make(chan struct{}, 100)
	testHookChecksum = func(rel string) {
		if rel != "app/page.tsx" || !armed.CompareAndSwap(true, false) {
			return
		}
		name, err := windows.UTF16PtrFromString(page)
		if err != nil {
			panic(err)
		}
		h, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			panic(err)
		}
		handle <- h
	}
	testHookHeld = func(rel string) {
		if rel == "app/page.tsx" {
			hit <- struct{}{}
		}
	}
	t.Cleanup(func() { testHookChecksum, testHookHeld = nil, nil })
	w = startWatcher(t, dir)
	var h windows.Handle
	released := false
	t.Cleanup(func() {
		if h != 0 && !released {
			windows.CloseHandle(h)
		}
	})
	release = func() {
		if h == 0 {
			select {
			case h = <-handle:
			case <-time.After(5 * time.Second):
				t.Fatal("the watcher did not hash the saved file")
			}
		}
		windows.CloseHandle(h)
		released = true
	}
	return w, func() { armed.Store(true) }, hit, release
}

// waitHit waits for the next hash that found the file held.
func waitHit(t *testing.T, hits <-chan struct{}) {
	t.Helper()
	select {
	case <-hits:
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not find the file held")
	}
}

// changedTo returns the checksum the watcher recorded as the content of app/page.tsx since the
// changes were last taken, "" for a file it took as removed, and whether it recorded a change.
func changedTo(w *watcher) (string, bool) {
	w.changes.mu.Lock()
	defer w.changes.mu.Unlock()
	sum, ok := w.changes.after["app/page.tsx"]
	return sum, ok
}

// Another program may hold a file without letting others open it, as an antivirus does when it
// scans a saved file, or Windows with a file that a rename replaces. Letting it go makes no event.
// The watcher looks at it again, and sees the save.
func TestWatcherLooksAgainAtAFileAnotherProgramHeld(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/page.tsx", "v0")
	w, arm, hits, release := holdPage(t, dir)
	arm()
	writeFile(t, dir, "app/page.tsx", "v1")
	waitHit(t, hits)
	release()
	want, err := checksum(filepath.Join(dir, "app", "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		sum, ok := changedTo(w)
		if ok && sum == want {
			break
		}
		if ok && sum == "" {
			t.Fatal("the file another program held was taken as removed")
		}
		if time.Now().After(deadline) {
			t.Fatalf("the watcher did not see the save once the file was free: recorded %q, %v", sum, ok)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A file held for longer than the watcher tries keeps what the watcher knew of it, and its next
// save is seen.
func TestWatcherKeepsAFileHeldForLong(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/page.tsx", "v0")
	w, arm, hits, release := holdPage(t, dir)
	arm()
	writeFile(t, dir, "app/page.tsx", "v1")
	for range 1 + heldTries {
		waitHit(t, hits)
	}
	time.Sleep(3 * heldWait) // the watcher gave up
	if sum, ok := changedTo(w); ok {
		t.Fatalf("a file another program held changed to %q in the watcher's eyes", sum)
	}
	release()
	writeFile(t, dir, "app/page.tsx", "v2")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w.sync(ctx)
	want, err := checksum(filepath.Join(dir, "app", "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if sum, ok := changedTo(w); !ok || sum != want {
		t.Fatalf("the next save was not seen: recorded %q, %v", sum, ok)
	}
}
