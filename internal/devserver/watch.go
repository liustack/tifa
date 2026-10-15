package devserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// changes collects the files whose content changed since they were last taken into a batch.
// The watcher adds to it, the rebuild loop takes batches from it.
type changes struct {
	mu       sync.Mutex
	before   map[string]string    // checksum when the file was last taken, "" when it did not exist
	after    map[string]string    // checksum now, "" when the file is gone
	first    map[string]time.Time // when the file first changed since it was last taken
	last     map[string]time.Time // when it changed last
	building bool                 // a build runs and may read any file at any moment
	touched  map[string]bool      // files that changed while a build ran
	ready    chan struct{}        // receives a value after add, holds at most one
}

func newChanges() *changes {
	return &changes{
		before: map[string]string{}, after: map[string]string{}, first: map[string]time.Time{}, last: map[string]time.Time{},
		touched: map[string]bool{}, ready: make(chan struct{}, 1),
	}
}

// setBuilding marks the time a build runs. A file that changes during that time counts as
// changed even if it ends up as before, because the build may have read it in between.
func (c *changes) setBuilding(b bool) {
	c.mu.Lock()
	c.building = b
	c.mu.Unlock()
}

// add records that path went from checksum before to checksum after.
func (c *changes) add(path, before, after string) {
	now := time.Now()
	c.mu.Lock()
	if _, ok := c.before[path]; !ok {
		c.before[path] = before
		c.first[path] = now
	}
	c.after[path] = after
	c.last[path] = now
	if c.building {
		c.touched[path] = true
	}
	c.mu.Unlock()
	select {
	case c.ready <- struct{}{}:
	default:
	}
}

// touch records that path changed, for a file whose content the watcher does not hash, such as
// one outside the application directory that Vite's dev server watches. It counts as modified.
func (c *changes) touch(path string) { c.add(path, "unknown", "changed") }

// take returns every file that changed, as a batch sorted by path. A file whose content ends up
// as it was when it was last taken is left out, unless it changed while a build ran.
func (c *changes) take() []change {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.takeLocked(func(string) bool { return true })
}

// due decides on the next batch at now. A file is settled once it went quiet for quiet, or once it
// kept changing for limit, such as a log: a file in the middle of a save does not go into a batch,
// and a file that never stops changing holds back neither itself nor the others for long. When
// every changed file is settled, or the oldest change waited limit, due takes the settled files as
// the batch. Otherwise batch is nil and wait says when to look again, 0 when nothing changed.
func (c *changes) due(now time.Time, quiet, limit time.Duration) (batch []change, wait time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.before) == 0 {
		return nil, 0
	}
	settled := func(p string) bool { return now.Sub(c.last[p]) >= quiet || now.Sub(c.first[p]) >= limit }
	oldest, all, some := now, true, false
	wait = limit
	for p := range c.before {
		if c.first[p].Before(oldest) {
			oldest = c.first[p]
		}
		if settled(p) {
			some = true
			continue
		}
		all = false
		wait = min(wait, c.last[p].Add(quiet).Sub(now), c.first[p].Add(limit).Sub(now))
	}
	if all || some && now.Sub(oldest) >= limit {
		return c.takeLocked(settled), 0
	}
	if some {
		wait = min(wait, oldest.Add(limit).Sub(now))
	}
	return nil, max(wait, time.Millisecond)
}

// takeLocked takes the files for which take reports true. Call it with c.mu held.
func (c *changes) takeLocked(take func(path string) bool) []change {
	batch := []change{}
	for p, before := range c.before {
		if !take(p) {
			continue
		}
		after, touched := c.after[p], c.touched[p]
		delete(c.before, p)
		delete(c.after, p)
		delete(c.first, p)
		delete(c.last, p)
		delete(c.touched, p)
		switch {
		case before == after && touched && after == "":
			batch = append(batch, change{p, removed})
		case before == after && touched:
			batch = append(batch, change{p, modified})
		case before == after:
		case before == "":
			batch = append(batch, change{p, created})
		case after == "":
			batch = append(batch, change{p, removed})
		default:
			batch = append(batch, change{p, modified})
		}
	}
	slices.SortFunc(batch, func(a, b change) int { return strings.Compare(a.path, b.path) })
	return batch
}

// skippedDirs are never watched, wherever they appear.
var skippedDirs = []string{".git", ".tifa", "node_modules"}

// systemWatcher is the system's file watcher: inotify through fsnotify on Linux, tifa's own
// kqueue watcher on macOS and the BSDs (watch_kqueue.go).
type systemWatcher struct {
	add    func(name string) error // watches a directory, or a file where watchesFiles
	remove func(name string) error
	close  func() error
	events <-chan fsnotify.Event
	errors <-chan error
}

// watcher watches every directory under root and reports the files that plan cares about
// whose content changed. Paths in its maps are relative to root with forward slashes, the
// root itself is ".". Only run touches the maps once newWatcher returns.
//
// Directories get a system watch while the budget allows it and the system has watches left,
// and where watchesFiles, so do their tracked files. The directories that do not are polled:
// their entries are listed and the files whose size, modification time or identity changed are
// hashed again. Every system watch counts one against the budget, and the watcher adds and
// removes each one itself, so watches holds exactly what the system watcher holds.
type watcher struct {
	root     string
	rootReal string                     // root with its links resolved
	ignore   []string                   // relative directories that are not watched, such as the build output
	sys      *systemWatcher             // nil when the system gave no watcher
	events   <-chan fsnotify.Event      // the system watcher's events, nil without one
	errs     <-chan error               // the system watcher's errors, nil without one
	add      func(name string) error    // adds a system watch
	remove   func(name string) error    // removes a system watch
	budget   int                        // how many system watches the watcher may hold
	watches  map[string]bool            // the paths that have a system watch
	fileOf   map[string]fileWatch       // the system watch of each tracked file, where watchesFiles
	full     bool                       // no more directories get a system watch, new ones are polled
	short    chan struct{}              // closed once full is set
	polled   map[string]bool            // directories that are polled
	dirty    map[string]bool            // directories whose entries changed, to list
	listAt   time.Time                  // when the dirty directories may be listed next
	stats    map[string]os.FileInfo     // tracked files as they were when last hashed
	minPoll  time.Duration              // shortest time between two polls
	files    map[string]string          // checksum of every tracked file
	dirs     map[string]os.FileInfo     // watched and polled directories, as they were when first seen
	kids     map[string]map[string]bool // tracked files and watched directories by the directory they are in
	real     map[string]string          // real path of each watched directory to its path under root
	realDir  map[string]string          // the other way: path under root of each watched directory to its real path
	linkDirs map[string]string          // watched directories that are links themselves, to the real path watched
	warn     func(string)
	warned   map[string]bool     // paths warned about, to warn once
	links    map[string][]string // real path of a linked file's target to the links under root
	linkOf   map[string]string   // link under root to the real path of its target
	extDirs  map[string]int      // directories watched for link targets, with how many links need each
	extWatch map[string]bool     // directories of extDirs that have a system watch
	changes  *changes
}

// fileWatch is the system watch of a tracked file: the path it was added for, and the file as it
// was then.
type fileWatch struct {
	name string
	info os.FileInfo
}

// newWatcher watches root and records the checksum of every tracked file, without reporting them.
// ignore lists directories relative to root that are skipped. warn receives problems that do not
// stop watching, such as a symbolic link loop or directories that are polled.
func newWatcher(root string, ignore []string, warn func(string)) (*watcher, error) {
	w, err := prepareWatcher(root, ignore, warn)
	if err != nil {
		return nil, err
	}
	if err := w.start(); err != nil {
		w.close()
		return nil, err
	}
	return w, nil
}

// newFSWatcher starts the system watcher. Tests replace it.
var newFSWatcher = newSystemWatcher

// prepareWatcher returns a watcher of root that has not looked at any file yet.
func prepareWatcher(root string, ignore []string, warn func(string)) (*watcher, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootReal, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	w := &watcher{
		root: abs, rootReal: rootReal, warn: warn, changes: newChanges(), short: make(chan struct{}),
		budget: watchBudget(), watches: map[string]bool{}, fileOf: map[string]fileWatch{}, polled: map[string]bool{}, dirty: map[string]bool{}, stats: map[string]os.FileInfo{}, minPoll: minPollInterval,
		files: map[string]string{}, dirs: map[string]os.FileInfo{}, kids: map[string]map[string]bool{}, real: map[string]string{}, realDir: map[string]string{}, linkDirs: map[string]string{}, warned: map[string]bool{},
		links: map[string][]string{}, linkOf: map[string]string{}, extDirs: map[string]int{}, extWatch: map[string]bool{},
	}
	for _, d := range ignore {
		w.ignore = append(w.ignore, path.Clean(filepath.ToSlash(d)))
	}
	fw, err := newFSWatcher()
	switch {
	case outOfWatches(err):
		// Such as inotify with no instance left for the user: poll everything.
		w.setFull()
		w.warn(fmt.Sprintf("starting the file watcher: %v: every directory is polled for changes", err))
	case err != nil:
		return nil, err
	default:
		w.sys, w.add, w.remove = fw, fw.add, fw.remove
		w.events, w.errs = fw.events, fw.errors
	}
	return w, nil
}

// start records the checksum of every tracked file, without reporting them.
func (w *watcher) start() error {
	if err := w.recover(w.rescan(".")); err != nil {
		return err
	}
	w.changes.take()
	return nil
}

// close stops the system watcher.
func (w *watcher) close() {
	if w.sys != nil {
		w.sys.close()
	}
}

// watchCount returns how many system watches the watcher holds.
func (w *watcher) watchCount() int { return len(w.watches) }

// linkPoll is how often the watcher looks at the links it follows. kqueue (macOS, BSD) watches
// what a link leads to, so removing or replacing the link itself gives no event.
const linkPoll = 500 * time.Millisecond

// testHookLinkPoll lets tests see each path the link poll looks at.
var testHookLinkPoll func(rel string)

// testHookChecksum lets tests see each file that is hashed.
var testHookChecksum func(rel string)

// testHookList lets tests see each directory listed after an event.
var testHookList func(rel string)

// Polling lists every polled directory and stats its tracked files, about 10µs per entry. A poll
// waits at least minPollInterval after the last one, and pollCostFactor times as long as the last
// one took, so polling a large tree takes at most a tenth of one core. The directories whose
// entries changed are listed under the same rule, without the minimum: a large directory that
// keeps growing costs no more, and a small one is listed right away.
const (
	minPollInterval = time.Second
	pollCostFactor  = 10
)

func (w *watcher) pollInterval(took time.Duration) time.Duration {
	return max(w.minPoll, took*pollCostFactor)
}

// run handles file events and polls until ctx is done, then stops watching.
func (w *watcher) run(ctx context.Context) error {
	defer w.close()
	links := time.NewTicker(linkPoll)
	defer links.Stop()
	poll := time.NewTimer(w.minPoll)
	defer poll.Stop()
	list := time.NewTimer(time.Hour)
	list.Stop()
	defer list.Stop()
	listing := false
	for {
		if len(w.dirty) > 0 && !listing {
			listing = true
			list.Reset(time.Until(w.listAt))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-list.C:
			listing = false
			start := time.Now()
			if err := w.recover(w.listDirty()); err != nil {
				return err
			}
			w.listAt = time.Now().Add(time.Since(start) * pollCostFactor)
		case <-links.C:
			if err := w.recover(w.checkLinks()); err != nil {
				return err
			}
		case <-poll.C:
			start := time.Now()
			if err := w.recover(w.poll()); err != nil {
				return err
			}
			poll.Reset(w.pollInterval(time.Since(start)))
		case ev, ok := <-w.events:
			if !ok {
				return errors.New("file watcher stopped")
			}
			if err := w.recover(w.handle(ev.Name, ev.Op)); err != nil {
				return err
			}
		case err, ok := <-w.errs:
			if !ok {
				return errors.New("file watcher stopped")
			}
			switch {
			case errors.Is(err, fsnotify.ErrEventOverflow):
				// Some events were dropped, so compare everything again.
				if err := w.rescan("."); err != nil {
					return err
				}
			case outOfWatches(err):
				// The error does not say which directory misses events.
				w.pollAll(err)
				if err := w.rescan("."); err != nil {
					return err
				}
			default:
				return fmt.Errorf("watching files: %w", err)
			}
		}
	}
}

// listDirty lists the directories whose entries changed since the last time.
func (w *watcher) listDirty() error {
	for dir := range w.dirty {
		delete(w.dirty, dir)
		if testHookList != nil {
			testHookList(dir)
		}
		if err := w.scan(dir, false); err != nil {
			return err
		}
	}
	return nil
}

// poll checks the polled directories, and the linked files whose target no system watch covers.
func (w *watcher) poll() error {
	for _, dir := range slices.Collect(maps.Keys(w.polled)) {
		if !w.polled[dir] {
			continue // gone with a directory polled before
		}
		if err := w.scan(dir, false); err != nil {
			return err
		}
	}
	for rel, target := range maps.Clone(w.linkOf) {
		if w.linkWatched(rel, target) {
			continue
		}
		if info, err := os.Stat(w.abs(rel)); err == nil && !w.statChanged(rel, info) {
			continue
		}
		if err := w.check(rel); err != nil {
			return err
		}
	}
	return nil
}

// statChanged reports whether the tracked file rel may have changed since it was last hashed.
func (w *watcher) statChanged(rel string, info os.FileInfo) bool {
	old, ok := w.stats[rel]
	return !ok || !os.SameFile(old, info) || old.Size() != info.Size() || !old.ModTime().Equal(info.ModTime())
}

// linkWatched reports whether a system watch reports edits of the target of the linked file rel:
// the watch of the link itself where watchesFiles, which follows it, or else the watch of the
// target's directory.
func (w *watcher) linkWatched(rel, target string) bool {
	if watchesFiles {
		_, ok := w.fileOf[rel]
		return ok
	}
	dir := filepath.Dir(target)
	if owner, ok := w.real[dir]; ok {
		return !w.polled[owner]
	}
	return w.extWatch[dir]
}

// watchDir adds a system watch for the directory dir, whose real path is real. It reports false
// when dir has to be polled instead, because the watch would go past the budget or the system
// has no watch left.
func (w *watcher) watchDir(dir, real string) (bool, error) {
	if w.full {
		return false, nil
	}
	if w.watchCount() >= w.budget {
		w.setFull()
		w.warn(fmt.Sprintf("watching %s would use up the system's file watches, so it and the directories found after it are polled for changes", dir))
		return false, nil
	}
	err := w.add(real)
	if outOfWatches(err) {
		w.setFull()
		w.warn(fmt.Sprintf("watching %s: %v: the system ran out of file watches, so it and the directories found after it are polled for changes", dir, err))
		w.spare()
		return false, nil
	}
	if err != nil {
		return false, err
	}
	w.watches[real] = true
	return true, nil
}

// watchFile adds a system watch for the tracked file rel where watchesFiles, unless its directory
// is polled. info is the file as check found it: a file replaced since its watch was added, such
// as by an atomic save, gets a new one. A watch that would go past the budget polls the directory
// instead. Running out of file descriptors comes back as the error, see recover.
func (w *watcher) watchFile(rel string, info os.FileInfo) error {
	if !watchesFiles || w.sys == nil {
		return nil
	}
	dir := path.Dir(rel)
	real, ok := w.realDir[dir]
	if !ok || w.polled[dir] {
		return nil
	}
	if old, ok := w.fileOf[rel]; ok {
		if os.SameFile(old.info, info) {
			return nil
		}
		w.unwatchFile(rel)
	}
	if w.watchCount() >= w.budget {
		w.pollDir(dir)
		w.warn(fmt.Sprintf("%s grew past what the system's file watches allow, so it and the directories found after it are polled for changes", dir))
		return nil
	}
	name := filepath.Join(real, path.Base(rel))
	err := w.add(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // gone again, the next event handles it
	}
	if err != nil {
		return err
	}
	w.watches[name] = true
	w.fileOf[rel] = fileWatch{name, info}
	return nil
}

// unwatchFile removes the system watch of the tracked file rel, if it has one.
func (w *watcher) unwatchFile(rel string) {
	if fw, ok := w.fileOf[rel]; ok {
		delete(w.fileOf, rel)
		w.dropWatch(fw.name)
	}
}

// pollDir polls the watched directory dir from now on, in place of its system watch and those of
// its files, and polls the directories found after it.
func (w *watcher) pollDir(dir string) {
	w.setFull()
	if real, ok := w.realDir[dir]; ok {
		w.dropWatch(real)
	}
	for kid := range w.kids[dir] {
		w.unwatchFile(kid)
	}
	w.polled[dir] = true
}

// setFull stops adding system watches: directories found from now on are polled. The system is
// short of watches or file descriptors, so the first call closes short, and tifa dev tells Vite's
// dev server to poll as well.
func (w *watcher) setFull() {
	if !w.full {
		w.full = true
		close(w.short)
	}
}

// spareWatches is how much the watcher gives back when the system runs out of watches, so tifa
// dev can still open files (kqueue) and other programs, editors among them, can still watch
// (inotify).
const spareWatches = 256

// spare polls watched directories in place of their system watches until spareWatches are free.
func (w *watcher) spare() {
	start := w.watchCount()
	for real, dir := range w.real {
		if start-w.watchCount() >= spareWatches {
			return
		}
		if w.watches[real] {
			w.pollDir(dir)
		}
	}
}

// pollAll stops the system watcher and polls every directory. Closing it frees every watch.
func (w *watcher) pollAll(err error) {
	w.setFull()
	if w.sys != nil {
		w.sys.close()
		w.sys, w.events, w.errs = nil, nil, nil
	}
	for real, dir := range w.real {
		if w.watches[real] {
			w.polled[dir] = true
		}
	}
	clear(w.watches)
	clear(w.fileOf)
	clear(w.extWatch)
	w.warn(fmt.Sprintf("watching files: %v: the system ran out of file watches, so every directory is polled for changes", err))
}

// recover polls everything and compares it with the disk again when err says the system ran out
// of watches or file descriptors, such as a file that cannot be opened to hash it. kqueue does
// not always report running out, so the next file the watcher opens may be the first to fail.
// Other errors come back as they are.
func (w *watcher) recover(err error) error {
	if err == nil || !outOfWatches(err) || w.sys == nil {
		return err
	}
	w.pollAll(err)
	return w.rescan(".")
}

// dropWatch removes the system watch of name, if it has one.
func (w *watcher) dropWatch(name string) {
	if w.watches[name] {
		_ = w.remove(name) // usually gone with the directory already
		delete(w.watches, name)
	}
}

// checkLinks brings every linked file and every linked directory up to date when the link no
// longer leads where it did. It looks only at the links: a directory reached through a link is
// watched by its real path like any other, and a change of a link above it rescans it.
func (w *watcher) checkLinks() error {
	for rel, target := range maps.Clone(w.linkOf) {
		if testHookLinkPoll != nil {
			testHookLinkPoll(rel)
		}
		if real, err := filepath.EvalSymlinks(w.abs(rel)); err != nil || real != target {
			if err := w.check(rel); err != nil {
				return err
			}
		}
	}
	for rel, real := range maps.Clone(w.linkDirs) {
		if testHookLinkPoll != nil {
			testHookLinkPoll(rel)
		}
		if now, err := filepath.EvalSymlinks(w.abs(rel)); err == nil && now == real {
			continue
		}
		if _, still := w.dirs[rel]; !still {
			continue // gone with a directory checked before
		}
		if err := w.scan(rel, false); err != nil {
			return err
		}
	}
	return nil
}

func (w *watcher) skipped(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if slices.Contains(skippedDirs, part) {
			return true
		}
	}
	for _, d := range w.ignore {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

func (w *watcher) abs(rel string) string { return filepath.Join(w.root, filepath.FromSlash(rel)) }

// under reports whether p is dir or inside it.
func under(dir, p string) bool { return dir == "." || p == dir || strings.HasPrefix(p, dir+"/") }

// watching reports whether dir is watched and is still the directory the watch was added for.
func (w *watcher) watching(dir string, info os.FileInfo) bool {
	known, ok := w.dirs[dir]
	return ok && os.SameFile(known, info)
}

// handle brings the state of one path up to date. Events only say where to look: the checksum
// decides whether a file changed, and a directory is checked against the one that was watched.
func (w *watcher) handle(name string, op fsnotify.Op) error {
	for _, link := range w.links[name] {
		if err := w.check(link); err != nil {
			return err
		}
	}
	rel, ok := w.relOf(name)
	if !ok || w.skipped(rel) {
		return nil
	}
	if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
		// Whatever is at rel now, the watch for the old directory or file is gone. A new one can
		// reuse the old one's inode, so identity alone cannot tell.
		w.unwatch(rel)
		w.unwatchFile(rel)
	}
	info, err := os.Stat(w.abs(rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		w.removeTree(rel)
		return nil
	case err != nil:
		return err
	case info.IsDir():
		w.dirty[rel] = true // listed by run, see listDirty
		return nil
	default:
		return w.check(rel)
	}
}

// relOf maps an event path to a path under root. Directories are watched by their real path,
// so events name real paths: a link to a directory is known by the link's path under root.
func (w *watcher) relOf(name string) (string, bool) {
	if rel, ok := w.real[name]; ok {
		return rel, true
	}
	if dir, ok := w.real[filepath.Dir(name)]; ok {
		return path.Join(dir, filepath.Base(name)), true
	}
	if r, err := filepath.Rel(w.root, name); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(r), true
	}
	return "", false
}

// rescan compares everything under rel with the disk, also in directories already watched.
func (w *watcher) rescan(rel string) error { return w.scan(rel, true) }

// scan watches dir, checks its files and scans its subdirectories. Without deep, it only looks at
// files and directories it does not know yet: known ones get events of their own. A directory
// that is new or replaced is scanned deep. Anything under dir that is gone counts as removed.
func (w *watcher) scan(dir string, deep bool) error {
	info, err := os.Stat(w.abs(dir))
	if errors.Is(err, fs.ErrNotExist) {
		w.removeTree(dir)
		return nil
	}
	if err != nil {
		return err
	}
	if !w.watching(dir, info) {
		w.unwatch(dir) // a replaced directory may still have the old watch
		real, err := filepath.EvalSymlinks(w.abs(dir))
		if err != nil {
			w.warnOnce(dir, fmt.Sprintf("not watching %s: %v", dir, err))
			return nil
		}
		if owner, ok := w.real[real]; ok {
			// A link to a directory that is already watched, possibly an ancestor.
			w.warnOnce(dir, fmt.Sprintf("not watching %s: it is the directory %s, which is already watched", dir, owner))
			return nil
		}
		// Watch the real path, which is also what the events name.
		watched, err := w.watchDir(dir, real)
		if errors.Is(err, fs.ErrNotExist) {
			w.removeTree(dir)
			return nil
		}
		if err != nil {
			return fmt.Errorf("watching %s: %w", dir, err)
		}
		if !watched {
			w.polled[dir] = true
		}
		w.dirs[dir] = info
		w.know(dir)
		w.real[real] = dir
		w.realDir[dir] = real
		if li, err := os.Lstat(w.abs(dir)); dir != "." && err == nil && li.Mode()&os.ModeSymlink != 0 {
			w.linkDirs[dir] = real
		}
		deep = true
	}
	entries, err := os.ReadDir(w.abs(dir))
	if errors.Is(err, fs.ErrNotExist) {
		w.removeTree(dir)
		return nil
	}
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, e := range entries {
		rel := path.Join(dir, e.Name())
		if w.skipped(rel) {
			continue
		}
		info, err := os.Stat(w.abs(rel)) // follows symlinks, like the build does
		if errors.Is(err, fs.ErrNotExist) {
			continue // also a link to nothing
		}
		if errors.Is(err, syscall.ELOOP) {
			w.warnOnce(rel, fmt.Sprintf("skipping %s: %v", rel, err))
			continue
		}
		if err != nil {
			return err
		}
		present[rel] = true
		if info.IsDir() {
			if deep || !w.watching(rel, info) {
				if err := w.scan(rel, deep); err != nil {
					return err
				}
			}
			continue
		}
		if _, known := w.files[rel]; known && !deep && !(w.polled[dir] && w.statChanged(rel, info)) && !w.replaced(rel, info) {
			continue
		}
		if err := w.check(rel); err != nil {
			return err
		}
	}
	// Remove what is no longer in dir: a vanished subdirectory takes its files along.
	for child := range w.kids[dir] {
		if !present[child] {
			w.removeTree(child)
		}
	}
	return nil
}

// replaced reports whether the tracked file rel is another file than its system watch watches,
// such as after an atomic save.
func (w *watcher) replaced(rel string, info os.FileInfo) bool {
	fw, ok := w.fileOf[rel]
	return ok && !os.SameFile(fw.info, info)
}

// know records rel, a tracked file or a watched directory, under the directory it is in.
func (w *watcher) know(rel string) {
	if rel == "." {
		return
	}
	parent := path.Dir(rel)
	if w.kids[parent] == nil {
		w.kids[parent] = map[string]bool{}
	}
	w.kids[parent][rel] = true
}

// forget undoes know once rel is neither a tracked file nor a watched directory.
func (w *watcher) forget(rel string) {
	if _, ok := w.files[rel]; ok {
		return
	}
	if _, ok := w.dirs[rel]; ok {
		return
	}
	parent := path.Dir(rel)
	delete(w.kids[parent], rel)
	if len(w.kids[parent]) == 0 {
		delete(w.kids, parent)
	}
}

// unwatch forgets the watch of dir, not of its subdirectories.
func (w *watcher) unwatch(dir string) {
	delete(w.dirs, dir)
	w.forget(dir)
	delete(w.linkDirs, dir)
	delete(w.polled, dir)
	for real, owner := range w.real {
		if owner == dir {
			delete(w.real, real)
			w.dropWatch(real)
		}
	}
	delete(w.realDir, dir)
	delete(w.warned, dir)
}

func (w *watcher) warnOnce(rel, msg string) {
	if !w.warned[rel] {
		w.warned[rel] = true
		w.warn(msg)
	}
}

// check hashes a tracked file and reports it when the checksum differs. It hashes on every call:
// size and modification time can stay the same while the content changes.
func (w *watcher) check(rel string) error {
	if kindOf(rel) == other {
		return nil
	}
	info, err := os.Stat(w.abs(rel))
	if errors.Is(err, fs.ErrNotExist) {
		w.removeTree(rel)
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if err := w.followLink(rel); err != nil {
		return err
	}
	// Watch before hashing, so an edit right after the hash gets an event.
	if err := w.watchFile(rel, info); err != nil {
		return err
	}
	if testHookChecksum != nil {
		testHookChecksum(rel)
	}
	sum, err := checksum(w.abs(rel))
	if errors.Is(err, fs.ErrNotExist) {
		w.removeTree(rel)
		return nil
	}
	if err != nil {
		return err
	}
	w.stats[rel] = info
	if old := w.files[rel]; sum != old {
		w.files[rel] = sum
		w.know(rel)
		w.changes.add(rel, old, sum)
	}
	return nil
}

// followLink keeps track of the target when rel is a symbolic link. inotify reports an edit of the
// target only to the target's directory, so a target outside the watched directories gets its
// directory watched as well.
func (w *watcher) followLink(rel string) error {
	target := ""
	if li, err := os.Lstat(w.abs(rel)); err == nil && li.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(w.abs(rel))
		if err != nil {
			return nil // gone again, the next event handles it
		}
		target = real
	}
	if w.linkOf[rel] == target {
		return nil
	}
	w.unlink(rel)
	if target == "" {
		return nil
	}
	w.linkOf[rel] = target
	w.links[target] = append(w.links[target], rel)
	if watchesFiles {
		return nil // the watch of the link itself follows it, see linkWatched
	}
	dir := filepath.Dir(target)
	if _, watched := w.real[dir]; watched {
		return nil
	}
	if w.extDirs[dir] == 0 {
		// Without a system watch the link is polled.
		watched, err := w.watchDir(dir, dir)
		if err != nil {
			watched = false
			w.warnOnce(rel, fmt.Sprintf("watching %s, the directory of the target of %s: %v: %s is polled for changes", dir, rel, err, rel))
		}
		w.extWatch[dir] = watched
	}
	w.extDirs[dir]++
	return nil
}

// unlink stops tracking the target of rel.
func (w *watcher) unlink(rel string) {
	target, ok := w.linkOf[rel]
	if !ok {
		return
	}
	delete(w.linkOf, rel)
	w.links[target] = slices.DeleteFunc(w.links[target], func(l string) bool { return l == rel })
	if len(w.links[target]) == 0 {
		delete(w.links, target)
	}
	dir := filepath.Dir(target)
	if n, ok := w.extDirs[dir]; ok {
		if n == 1 {
			delete(w.extDirs, dir)
			delete(w.extWatch, dir)
			w.dropWatch(dir)
		} else {
			w.extDirs[dir] = n - 1
		}
	}
}

// removeTree forgets rel and everything under it, reporting the tracked files as removed.
func (w *watcher) removeTree(rel string) {
	for p, sum := range w.files {
		if under(rel, p) {
			delete(w.files, p)
			delete(w.stats, p)
			w.forget(p)
			w.unlink(p)
			w.unwatchFile(p)
			w.changes.add(p, sum, "")
		}
	}
	for d := range w.dirs {
		if under(rel, d) {
			w.unwatch(d)
		}
	}
}

func checksum(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
