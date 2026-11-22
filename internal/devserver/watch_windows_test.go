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

// Another program may hold a file without letting others read it for a moment, as Windows does
// with a file that a rename replaces. The watcher goes on, and sees the file once it is free.
func TestWatcherGoesOnWhileAnotherProgramHoldsAFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/page.tsx", "v0")
	page := filepath.Join(dir, "app", "page.tsx")
	var armed atomic.Bool
	held := make(chan windows.Handle, 1)
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
		held <- h
	}
	t.Cleanup(func() { testHookChecksum = nil })
	w := startWatcher(t, dir)
	armed.Store(true)
	writeFile(t, dir, "app/page.tsx", "v1")
	select {
	case h := <-held:
		windows.CloseHandle(h)
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not look at the saved file")
	}
	writeFile(t, dir, "app/page.tsx", "v2")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w.sync(ctx)
	select {
	case <-w.stopped:
		t.Fatal("the watcher stopped")
	default:
	}
	if w.changes.outstanding(w.changes.mark()) == nil {
		t.Fatal("after the sync no change waits for a rebuild")
	}
}
