package devserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A sync returns once the watcher handled the event of a save made before it, so the change is
// recorded, however soon after the save it is called. Atomic saves, which rename a new file over
// the old one, count too.
func TestWatcherSyncSeesASaveMadeBefore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/page.tsx", "v0")
	w := startWatcher(t, dir)
	for i := range 50 {
		if i%2 == 0 {
			writeFile(t, dir, "app/page.tsx", fmt.Sprintf("v%d", i+1))
		} else {
			writeFile(t, dir, "app/page.tsx.tmp", fmt.Sprintf("v%d", i+1))
			if err := os.Rename(filepath.Join(dir, "app/page.tsx.tmp"), filepath.Join(dir, "app/page.tsx")); err != nil {
				t.Fatal(err)
			}
		}
		w.sync(context.Background())
		if w.changes.outstanding(w.changes.mark()) == nil {
			t.Fatalf("save %d: after the sync no change waits for a rebuild", i+1)
		}
		w.changes.take()
	}
}

// Syncs that arrive while one waits for the event of its write all end, also with many at once.
func TestWatcherSyncsAtOnceAllEnd(t *testing.T) {
	w := startWatcher(t, t.TempDir())
	done := make(chan struct{})
	for range 20 {
		go func() {
			w.sync(context.Background())
			done <- struct{}{}
		}()
	}
	for range 20 {
		select {
		case <-done:
		case <-time.After(syncWait / 2):
			t.Fatal("a sync did not end")
		}
	}
}

// Without a system watcher every directory is polled and no event comes: a sync ends at once.
func TestWatcherSyncEndsWhenPolling(t *testing.T) {
	outErr := outOfWatchesErrors(t)[0]
	newFSWatcher = func() (*systemWatcher, error) { return nil, outErr }
	defer func() { newFSWatcher = newSystemWatcher }()
	w := startWatcher(t, t.TempDir())
	ended := make(chan struct{})
	go func() {
		w.sync(context.Background())
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(syncWait / 2):
		t.Fatal("a sync waits though no event comes")
	}
}

// The file of the syncs goes away with the watcher.
func TestWatcherRemovesTheSyncFile(t *testing.T) {
	w, err := newWatcher(t.TempDir(), nil, func(msg string) { t.Log(msg) })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.run(ctx) }()
	w.sync(ctx)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if w.sy.dir == "" {
		t.Fatal("the sync made no file")
	}
	if _, err := os.Stat(w.sy.dir); !os.IsNotExist(err) {
		t.Fatalf("the directory of the sync file is still there: %v", err)
	}
}
