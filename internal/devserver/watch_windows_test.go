//go:build windows

package devserver

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// holdPage starts a watcher of dir that, while hold is set, finds app/page.tsx held by another
// program when it hashes it, and sends to hits each time. A file opened by another program with
// no sharing does not do it: Go opens files with FILE_FLAG_BACKUP_SEMANTICS, and on the Windows
// runners of GitHub, whose account may back up files, that open goes through.
func holdPage(t *testing.T, dir string) (w *watcher, hold *atomic.Bool, hits <-chan struct{}) {
	t.Helper()
	page := filepath.Join(dir, "app", "page.tsx")
	hold = new(atomic.Bool)
	hit := make(chan struct{}, 100)
	openToHash = func(name string) (*os.File, error) {
		if name == page && hold.Load() {
			hit <- struct{}{}
			return nil, &fs.PathError{Op: "open", Path: name, Err: errSharingViolation}
		}
		return os.Open(name)
	}
	t.Cleanup(func() { openToHash = os.Open })
	return startWatcher(t, dir), hold, hit
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

// Another program may hold a file without letting others open it, as an antivirus may when it
// scans a saved file, or Windows with a file that a rename replaces. Letting it go makes no event.
// The watcher looks at it again and sees the save, and a sync waits for that.
func TestWatcherLooksAgainAtAFileAnotherProgramHeld(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/page.tsx", "v0")
	w, hold, hits := holdPage(t, dir)
	hold.Store(true)
	writeFile(t, dir, "app/page.tsx", "v1")
	waitHit(t, hits)
	if sum, ok := changedTo(w); ok {
		t.Fatalf("a file another program held changed to %q in the watcher's eyes", sum)
	}
	synced := make(chan struct{})
	go func() {
		w.sync(context.Background())
		close(synced)
	}()
	waitHit(t, hits) // the watcher looked again, while the sync waits
	select {
	case <-synced:
		t.Fatal("the sync ended before the watcher could hash the held file")
	default:
	}
	hold.Store(false)
	select {
	case <-synced:
	case <-time.After(5 * time.Second):
		t.Fatal("the sync did not end once the file was free")
	}
	want, err := checksum(filepath.Join(dir, "app", "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if sum, ok := changedTo(w); !ok || sum != want {
		t.Fatalf("the save was not seen once the file was free: recorded %q, %v", sum, ok)
	}
}

// A file held for longer than the watcher tries keeps what the watcher knew of it, and its next
// save is seen.
func TestWatcherKeepsAFileHeldForLong(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/page.tsx", "v0")
	w, hold, hits := holdPage(t, dir)
	hold.Store(true)
	writeFile(t, dir, "app/page.tsx", "v1")
	for range 1 + heldTries {
		waitHit(t, hits)
	}
	time.Sleep(3 * heldWait) // the watcher gave up
	if sum, ok := changedTo(w); ok {
		t.Fatalf("a file another program held changed to %q in the watcher's eyes", sum)
	}
	hold.Store(false)
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
