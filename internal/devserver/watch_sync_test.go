package devserver

import (
	"sync"

	"context"
	"fmt"
	"github.com/fsnotify/fsnotify"
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

// After a sync ends without the event of its write, because it waited syncWait or events were
// lost, that event may still come later. It must not end a later sync, which waits for its own.
func TestWatcherSyncIgnoresALateEventOfAnEarlierWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		lose func(errs chan error) // ends the first sync without its event, nil to let it wait syncWait
	}{
		{"waited too long", nil},
		{"events lost", func(errs chan error) { errs <- fsnotify.ErrEventOverflow }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events, errs := make(chan fsnotify.Event), make(chan error)
			var mu sync.Mutex
			var added []string
			newFSWatcher = func() (*systemWatcher, error) {
				return &systemWatcher{
					add: func(name string) error {
						mu.Lock()
						added = append(added, name)
						mu.Unlock()
						return nil
					},
					remove: func(string) error { return nil },
					close:  func() error { return nil },
					events: events, errors: errs,
				}, nil
			}
			defer func() { newFSWatcher = newSystemWatcher }()
			wait := syncWait
			defer func() { syncWait = wait }()
			syncWait = 50 * time.Millisecond
			if tc.lose != nil {
				syncWait = time.Hour
			}
			w := startWatcher(t, t.TempDir())
			// The name the events of the sync file have: the last watch added is that of the file,
			// or of its folder.
			syncName := func() string {
				mu.Lock()
				defer mu.Unlock()
				last := added[len(added)-1]
				if watchesFiles {
					return last
				}
				return filepath.Join(last, "sync")
			}
			start := func() chan struct{} {
				done := make(chan struct{})
				go func() {
					w.sync(context.Background())
					close(done)
				}()
				return done
			}

			first := start()
			time.Sleep(20 * time.Millisecond) // the watcher wrote for it
			late := syncName()
			if tc.lose != nil {
				tc.lose(errs)
			}
			select {
			case <-first:
			case <-time.After(5 * time.Second):
				t.Fatal("the first sync did not end without its event")
			}

			syncWait = time.Hour
			second := start()
			time.Sleep(20 * time.Millisecond) // the watcher wrote for it
			events <- fsnotify.Event{Name: late, Op: fsnotify.Write}
			select {
			case <-second:
				t.Fatal("the late event of the first write ended the second sync")
			case <-time.After(100 * time.Millisecond):
			}
			events <- fsnotify.Event{Name: syncName(), Op: fsnotify.Write}
			select {
			case <-second:
			case <-time.After(5 * time.Second):
				t.Fatal("the event of its own write did not end the second sync")
			}
		})
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
