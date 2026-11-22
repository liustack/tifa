package devserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// A page request asks the watcher to catch up before it looks for changes that wait for a
// rebuild: a file saved just before the request has an event the watcher may not have handled yet.
// The watcher writes to a file of its own outside the root, which the same system watcher
// watches. The system reports events in the order they happened, so once the event of that write
// arrives, the watcher has handled the events of every save made before the request. This costs
// one write and one event per request (two on Windows, see syncRounds), whatever the size of the
// app, and no look at other files.

// syncWait is how long the watcher waits for the event of its own write. The system never drops it
// without saying so, and it takes well under a millisecond. Past syncWait the requests go on, so a
// lost event costs them that long at most. Tests shorten it.
var syncWait = time.Second

// syncByName says that a write to an open file is no event the watcher can wait for. Windows
// reports the change of a file's last write time only once its cache is written to the disk,
// which may be long after the write, and fsnotify asks for no other change of a file's content.
// The name of a new file is reported at once. So on Windows each sync makes a new empty file,
// sync, sync-2 and so on, and waits for the event of its name.
const syncByName = runtime.GOOS == "windows"

// syncRounds is how many writes a sync waits for, each after the event of the one before. On
// Windows fsnotify reads each directory with a ReadDirectoryChangesW of its own, and starts the
// next read of a directory only once it delivered the events of the last one. Windows holds
// back the changes made meanwhile until that read starts, so the event of a save can come after
// that of the sync's write, in a directory whose read was waiting. fsnotify handles the reads
// one at a time, in the order they ended, and starts each again before the next: once the event
// of the first write arrives, every change made before it has been reported, or will be before
// the event of a second write.
func syncRounds() int {
	if runtime.GOOS == "windows" {
		return 2
	}
	return 1
}

// syncer is the watcher's side of sync. Only run touches it.
type syncer struct {
	dir  string          // temporary directory that holds file, "" until the first sync
	file *os.File        // the file written to, nil until the first sync and with syncByName
	name string          // the name of the event of the last write, "" until the first sync
	made int             // the files made in dir, with syncByName
	off  bool            // syncs cannot work, such as without a system watcher: they end at once
	sent []chan struct{} // the syncs that wait for the event of the last write
	left int             // the writes the syncs in sent still wait for the event of, see syncRounds
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

// catchUp is what the loop asks before it takes a batch: it returns once the watcher handled the
// events of the changes made before, as sync does, and looked at the polled directories and the
// links again. A file saved in place may have been seen by a poll in the middle of the save, and
// the next poll is far off: waiting for it would leave the empty file as a change of its own,
// and the written file another one. This costs one poll per batch. ctx and the stop of the
// watcher end the wait.
func (w *watcher) catchUp(ctx context.Context) {
	done := make(chan struct{})
	select {
	case w.looks <- done:
	case <-ctx.Done():
		return
	case <-w.stopped:
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
		return
	case <-w.stopped:
		return
	}
	w.sync(ctx)
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
	if err := w.openSync(); err != nil {
		w.syncFailed(err)
		return
	}
	w.sy.sent, w.sy.next = w.sy.next, nil
	w.sy.left = syncRounds()
	w.writeRound()
}

// writeRound makes the next write the syncs in sent wait for, and starts to wait syncWait for its
// event.
func (w *watcher) writeRound() {
	if err := w.touchSync(); err != nil {
		w.syncFailed(err)
		return
	}
	if w.sy.timeout == nil {
		w.sy.timeout = time.NewTimer(syncWait)
	} else {
		w.sy.timeout.Reset(syncWait)
	}
}

// syncFailed ends the syncs, now and from then on, after the file of the syncs could not be
// written or watched.
func (w *watcher) syncFailed(err error) {
	w.warnOnce(fmt.Sprintf("page requests do not wait for the watcher to see a file saved just before: %v", err))
	w.sy.off = true
	w.endSyncs()
}

// touchSync makes the change whose event ends the syncs: a write to the file, or with syncByName a
// new file, after which the one before goes.
func (w *watcher) touchSync() error {
	if !syncByName {
		_, err := w.sy.file.WriteAt([]byte{0}, 0)
		return err
	}
	w.sy.made++
	name := filepath.Join(w.sy.dir, "sync")
	if w.sy.made > 1 {
		name += "-" + strconv.Itoa(w.sy.made)
	}
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if w.sy.name != "" {
		// Its event, a removal, names a file that is no longer the one of the sync.
		if err := os.Remove(w.sy.name); err != nil {
			return err
		}
	}
	w.sy.name = name
	return nil
}

// openSync creates and watches the file on the first sync.
func (w *watcher) openSync() error {
	if w.sy.dir != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "tifa-dev-")
	if err != nil {
		return err
	}
	if syncByName {
		if err := w.sys.add(dir); err != nil {
			os.RemoveAll(dir)
			return err
		}
		w.sy.dir = dir
		return nil
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
	w.sy.dir, w.sy.file, w.sy.name = dir, f, f.Name()
	return nil
}

// isSync reports whether the event named name is the one of the watcher's own write.
func (w *watcher) isSync(name string) bool {
	return w.sy.name != "" && name == w.sy.name
}

// synced handles the event of the last write: it writes again while the syncs in sent wait for
// another round (see syncRounds), and ends them otherwise, as syncsWaited does.
func (w *watcher) synced() {
	if w.sy.left--; w.sy.left > 0 {
		w.writeRound()
		return
	}
	w.syncsWaited()
}

// syncsWaited ends the syncs that waited for the event of the last write, and writes again for
// those that came since.
func (w *watcher) syncsWaited() {
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
	if w.sy.dir == "" {
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
	if w.sy.file != nil {
		w.sy.file.Close()
	}
	os.RemoveAll(w.sy.dir)
	w.sy.file, w.sy.dir, w.sy.name, w.sy.made = nil, "", "", 0
}

// closeSync removes the file and its directory.
func (w *watcher) closeSync() {
	if w.sy.timeout != nil {
		w.sy.timeout.Stop()
	}
	if w.sy.file != nil {
		w.sy.file.Close()
	}
	if w.sy.dir != "" {
		os.RemoveAll(w.sy.dir)
	}
}
