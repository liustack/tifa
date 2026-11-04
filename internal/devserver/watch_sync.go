package devserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// A page request asks the watcher to catch up before it looks for changes that wait for a
// rebuild: a file saved just before the request has an event the watcher may not have handled yet.
// The watcher writes to a file of its own outside the root, which the same system watcher
// watches. The system reports events in the order they happened, so once the event of that write
// arrives, the watcher has handled the events of every save made before the request. This costs
// one write and one event per request, whatever the size of the app, and no look at other files.

// syncWait is how long the watcher waits for the event of its own write. The system never drops it
// without saying so, and it takes well under a millisecond. Past syncWait the requests go on, so a
// lost event costs them that long at most. Tests shorten it.
var syncWait = time.Second

// syncer is the watcher's side of sync. Only run touches it.
type syncer struct {
	dir  string          // temporary directory that holds file, "" until the first sync
	file *os.File        // the file written to, nil until the first sync
	off  bool            // syncs cannot work, such as without a system watcher: they end at once
	sent []chan struct{} // the syncs that wait for the event of the last write
	next []chan struct{} // the syncs that came after that write, which wait for the next one
	// timeout fires syncWait after the last write, nil while no write waits for its event.
	timeout *time.Timer
}

// sync returns once the watcher handled the events of the changes made before sync was called, or
// ctx is done, or the watcher stopped. A change in a polled directory is not seen before the next
// poll, and a file new to a directory is seen once the directory is listed (see listDirty).
func (w *watcher) sync(ctx context.Context) {
	done := make(chan struct{})
	select {
	case w.syncs <- done:
	case <-ctx.Done():
		return
	case <-w.stopped:
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	case <-w.stopped:
	}
}

// syncTimeout returns the channel of the timer that waits for the event of the last write, nil
// while none waits.
func (w *watcher) syncTimeout() <-chan time.Time {
	if w.sy.timeout == nil || len(w.sy.sent) == 0 {
		return nil
	}
	return w.sy.timeout.C
}

// addSync takes a sync request and writes for it, unless a write already waits for its event:
// done then waits for the next write, as its changes may have come after that one.
func (w *watcher) addSync(done chan struct{}) {
	w.sy.next = append(w.sy.next, done)
	if len(w.sy.sent) == 0 {
		w.writeSync()
	}
}

// writeSync writes to the file for the syncs in next. When the file cannot be written or watched,
// syncs end at once from then on.
func (w *watcher) writeSync() {
	if w.sy.off || w.sys == nil {
		// Without a system watcher every directory is polled, and no event comes.
		w.endSyncs()
		return
	}
	err := w.openSync()
	if err == nil {
		_, err = w.sy.file.WriteAt([]byte{0}, 0)
	}
	if err != nil {
		w.warnOnce(fmt.Sprintf("page requests do not wait for the watcher to see a file saved just before: %v", err))
		w.sy.off = true
		w.endSyncs()
		return
	}
	w.sy.sent, w.sy.next = w.sy.next, nil
	if w.sy.timeout == nil {
		w.sy.timeout = time.NewTimer(syncWait)
	} else {
		w.sy.timeout.Reset(syncWait)
	}
}

// openSync creates and watches the file on the first sync.
func (w *watcher) openSync() error {
	if w.sy.file != nil {
		return nil
	}
	dir, err := os.MkdirTemp("", "tifa-dev-")
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "sync"))
	if err == nil {
		// inotify reports the writes to a file through the watch of its directory, kqueue
		// through a watch of the file itself.
		watch := dir
		if watchesFiles {
			watch = f.Name()
		}
		if err = w.sys.add(watch); err != nil {
			f.Close()
		}
	}
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	w.sy.dir, w.sy.file = dir, f
	return nil
}

// isSync reports whether the event named name is the one of the watcher's own write.
func (w *watcher) isSync(name string) bool {
	return w.sy.file != nil && name == w.sy.file.Name()
}

// synced ends the syncs that waited for the event of the last write, and writes again for those
// that came since.
func (w *watcher) synced() {
	for _, done := range w.sy.sent {
		close(done)
	}
	w.sy.sent = nil
	if len(w.sy.next) > 0 {
		w.writeSync()
	}
}

// endSyncs ends every sync that waits, such as when events were lost and the watcher compared
// everything again, or the system watcher is gone.
func (w *watcher) endSyncs() {
	for _, done := range w.sy.sent {
		close(done)
	}
	for _, done := range w.sy.next {
		close(done)
	}
	w.sy.sent, w.sy.next = nil, nil
}

// dropSync stops using the file once a sync ended without the event of its last write, after
// syncWait or when events were lost. That event may still come, and it must not count for the
// next write: the next sync writes to a new file, and the events of this one are ignored.
func (w *watcher) dropSync() {
	if w.sy.file == nil {
		return
	}
	watch := w.sy.dir
	if watchesFiles {
		watch = w.sy.file.Name()
	}
	if w.sys != nil {
		if err := w.sys.remove(watch); err != nil {
			w.warnOnce(fmt.Sprintf("removing the watch of %s: %v", watch, err))
		}
	}
	w.sy.file.Close()
	os.RemoveAll(w.sy.dir)
	w.sy.file, w.sy.dir = nil, ""
}

// closeSync removes the file and its directory.
func (w *watcher) closeSync() {
	if w.sy.timeout != nil {
		w.sy.timeout.Stop()
	}
	if w.sy.file != nil {
		w.sy.file.Close()
		os.RemoveAll(w.sy.dir)
	}
}
