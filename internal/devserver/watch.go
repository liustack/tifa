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

// watcher keeps a snapshot of what is under root and reports the files that plan cares about
// whose content changed. System events, polls and the link poll only say where to look: the
// watcher then looks at that place on the disk and brings the snapshot in line with what it
// finds there, whatever it found before. Paths are relative to root with forward slashes, the
// root itself is ".". Only run touches the snapshot once newWatcher returns.
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
	full     bool                       // no more directories get a system watch, new ones are polled
	short    chan struct{}              // closed once full is set
	nodes    map[string]*node           // the snapshot: what each path was when the watcher last looked
	kids     map[string]map[string]bool // the nodes in each directory node
	owner    map[string]string          // the directory node of each real directory
	polled   map[string]bool            // directory nodes that are polled
	dirty    map[string]bool            // directory nodes whose entries changed, to list
	listAt   time.Time                  // when the dirty directories may be listed next
	minPoll  time.Duration              // shortest time between two polls
	warn     func(string)
	warned   map[string]bool   // the warnings given, by their text, to give each once
	was      map[string]string // while renew runs, the checksums of the files it dropped
	changes  *changes
}

// nodeKind is what a path in the snapshot is.
type nodeKind int

const (
	fileNode  nodeKind = iota // a file that plan cares about
	dirNode                   // a directory the watcher looks into
	aliasNode                 // a directory whose real directory a directory node already covers, through a link
	linkNode                  // a link that leads nowhere, in a loop, or to a file plan does not care about
)

// node is what the watcher found at a path when it last looked.
type node struct {
	kind  nodeKind
	link  bool        // the path is a symbolic link
	info  os.FileInfo // what the path leads to, links followed, nil for a linkNode
	real  string      // the real path of a directory or alias node
	sum   string      // the checksum of a file node
	watch string      // the system watch of a file node, where watchesFiles, "" without one
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
		budget: watchBudget(), watches: map[string]bool{}, minPoll: minPollInterval,
		nodes: map[string]*node{}, kids: map[string]map[string]bool{}, owner: map[string]string{},
		polled: map[string]bool{}, dirty: map[string]bool{}, warned: map[string]bool{},
	}
	for _, d := range ignore {
		w.ignore = append(w.ignore, path.Clean(filepath.ToSlash(d)))
	}
	fw, err := newFSWatcher()
	switch {
	case outOfWatches(err):
		// Such as inotify with no instance left for the user: poll everything.
		w.setFull()
		w.warnOnce(fmt.Sprintf("starting the file watcher: %v: every directory is polled for changes", err))
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

// linkPoll is how often the watcher looks at the links under root. Replacing what a link leads
// to, or a target outside the watched directories, gives no event.
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
		if err := w.relist(dir); err != nil {
			return err
		}
	}
	return nil
}

// poll lists the polled directories.
func (w *watcher) poll() error {
	for _, dir := range slices.Collect(maps.Keys(w.polled)) {
		if err := w.relist(dir); err != nil {
			return err
		}
	}
	return nil
}

// checkLinks looks again at each link under root that leads elsewhere now, or whose target
// changed. Every link that leads to a directory another node covers is looked at again, which
// decides anew whether the watcher looks into it.
func (w *watcher) checkLinks() error {
	var links []string
	for rel, n := range w.nodes {
		if n.link {
			links = append(links, rel)
		}
	}
	slices.Sort(links) // a link before the links under it
	for _, rel := range links {
		n := w.nodes[rel]
		if n == nil {
			continue // gone with a link before it
		}
		if testHookLinkPoll != nil {
			testHookLinkPoll(rel)
		}
		if w.linkChanged(rel, n) {
			if err := w.look(rel, hinted); err != nil {
				return err
			}
		}
	}
	return nil
}

// linkChanged reports whether the link at rel may lead to something else than its node says.
func (w *watcher) linkChanged(rel string, n *node) bool {
	info, err := os.Stat(w.abs(rel))
	switch {
	case err != nil:
		return n.kind != linkNode
	case n.kind == linkNode || n.kind == aliasNode:
		return true
	case n.kind == fileNode:
		return statChanged(n.info, info)
	}
	real, err := filepath.EvalSymlinks(w.abs(rel))
	return err != nil || real != n.real || !os.SameFile(n.info, info)
}

// statChanged reports whether a file may have other content than when it was info.
func statChanged(info, now os.FileInfo) bool {
	return !os.SameFile(info, now) || info.Size() != now.Size() || !info.ModTime().Equal(now.ModTime())
}

// how says why the watcher looks at a path.
type how int

const (
	listed how = iota // its directory is listed: a file is hashed again only when it looks changed
	hinted            // an event or a poll points at it: a file is hashed again
	rescan            // everything under it is compared again, directories that look the same are listed
)

// rescan compares everything under rel with the disk.
func (w *watcher) rescan(rel string) error { return w.look(rel, rescan) }

// look brings the snapshot of rel in line with the disk. A path that is gone takes everything
// under it along. A path that is something else now than its node says, such as a file that
// became a directory or a link that leads elsewhere, is dropped and taken in as new.
func (w *watcher) look(rel string, h how) error {
	if rel != "." && w.skipped(rel) {
		return nil
	}
	li, err := os.Lstat(w.abs(rel))
	if vanished(err) {
		w.drop(rel)
		return nil
	}
	if err != nil {
		return err
	}
	link := li.Mode()&os.ModeSymlink != 0
	info, err := os.Stat(w.abs(rel)) // follows links, like the build does
	switch {
	case vanished(err) || link && errors.Is(err, syscall.EINVAL):
		// A link that leads nowhere, until its target comes back. macOS may fail with EINVAL
		// while the link is being replaced.
		w.setLink(rel, link)
	case err != nil && link:
		w.warnOnce(fmt.Sprintf("skipping %s: %v", rel, err)) // such as a loop
		w.setLink(rel, link)
	case err != nil:
		return err
	case info.IsDir():
		return w.lookDir(rel, info, link, h)
	case info.Mode().IsRegular() && kindOf(rel) != other:
		return w.check(rel, info, link, h)
	default:
		w.setLink(rel, link)
	}
	return nil
}

// vanished reports whether err says a path is no longer there, also because a directory on its
// way became a file.
func vanished(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// setLink makes rel a link node if it is a link, and drops it from the snapshot otherwise.
func (w *watcher) setLink(rel string, link bool) {
	if n := w.nodes[rel]; n != nil && n.kind == linkNode && link {
		return
	}
	w.drop(rel)
	if link {
		w.set(rel, &node{kind: linkNode, link: true})
	}
}

// lookDir brings the snapshot of the directory rel in line with the disk, info being what it is
// now. A directory the snapshot holds as it is stays, listed again only for a rescan. Any other
// one becomes a directory node, watched and listed, unless a directory node already covers its
// real directory.
func (w *watcher) lookDir(rel string, info os.FileInfo, link bool, h how) error {
	real, err := filepath.EvalSymlinks(w.abs(rel))
	if err != nil {
		if !vanished(err) && !errors.Is(err, syscall.EINVAL) {
			w.warnOnce(fmt.Sprintf("not watching %s: %v", rel, err))
		}
		w.setLink(rel, link) // changed since the stat: a link is looked at again by the link poll
		return nil
	}
	if n := w.nodes[rel]; n != nil && n.kind == dirNode && n.real == real && os.SameFile(n.info, info) {
		n.link = link
		if h == rescan {
			return w.list(rel, h)
		}
		return nil
	}
	return w.renew(func() error { return w.takeDir(rel, info, link, real) })
}

// takeDir takes the directory rel into the snapshot as new.
func (w *watcher) takeDir(rel string, info os.FileInfo, link bool, real string) error {
	w.drop(rel)
	if owner, ok := w.owner[real]; ok {
		// A link to a directory that is already watched, possibly an ancestor.
		w.set(rel, &node{kind: aliasNode, link: link, info: info, real: real})
		w.warnOnce(fmt.Sprintf("not watching %s: it is the directory %s, which is already watched", rel, owner))
		return nil
	}
	watched, err := w.watchDir(rel, real)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // gone again, the next event looks
	}
	if err != nil {
		return fmt.Errorf("watching %s: %w", rel, err)
	}
	w.set(rel, &node{kind: dirNode, link: link, info: info, real: real})
	w.owner[real] = rel
	if !watched {
		w.polled[rel] = true
	}
	return w.list(rel, rescan)
}

// relist lists the directory node dir again, once a look at dir itself found it unchanged.
func (w *watcher) relist(dir string) error {
	n := w.nodes[dir]
	if n == nil || n.kind != dirNode {
		return nil
	}
	if err := w.look(dir, listed); err != nil {
		return err
	}
	if w.nodes[dir] != n {
		return nil // gone, or looked at as new and listed already
	}
	return w.list(dir, listed)
}

// list brings the snapshot of the entries of the directory node dir in line with the disk.
func (w *watcher) list(dir string, h how) error {
	entries, err := os.ReadDir(w.abs(dir))
	if vanished(err) {
		w.drop(dir)
		return nil
	}
	if err != nil {
		return err
	}
	// What is gone goes first, and directory nodes before the other entries: a directory that
	// is gone or changed lets go of its real directory before a link to it is looked at.
	present := map[string]bool{}
	var dirs, others []string
	for _, e := range entries {
		rel := path.Join(dir, e.Name())
		switch {
		case w.skipped(rel):
			continue
		case w.isDir(rel):
			dirs = append(dirs, rel)
		default:
			others = append(others, rel)
		}
		present[rel] = true
	}
	for kid := range w.kids[dir] {
		if !present[kid] {
			w.drop(kid)
		}
	}
	for _, rel := range append(dirs, others...) {
		if err := w.look(rel, h); err != nil {
			return err
		}
	}
	return nil
}

// isDir reports whether rel is a directory node.
func (w *watcher) isDir(rel string) bool {
	n := w.nodes[rel]
	return n != nil && n.kind == dirNode
}

// check brings the snapshot of the tracked file rel in line with the disk, info being what it is
// now. Listed, it hashes the file only when it looks changed: new, replaced, or in a polled
// directory with another size or modification time. Otherwise it always hashes: size and
// modification time can stay the same while the content changes.
func (w *watcher) check(rel string, info os.FileInfo, link bool, h how) error {
	n := w.nodes[rel]
	if n != nil && n.kind != fileNode {
		w.drop(rel) // such as a directory that became a file
		n = nil
	}
	if n != nil && h == listed && os.SameFile(n.info, info) && !(w.polled[path.Dir(rel)] && statChanged(n.info, info)) {
		return nil
	}
	if n == nil {
		n = &node{kind: fileNode, sum: w.was[rel]}
		delete(w.was, rel)
		w.set(rel, n)
	}
	n.link = link
	// Watch before hashing, so an edit right after the hash gets an event.
	if err := w.watchFile(rel, n, info); err != nil {
		return err
	}
	if testHookChecksum != nil {
		testHookChecksum(rel)
	}
	sum, err := checksum(w.abs(rel))
	if vanished(err) || errors.Is(err, syscall.EISDIR) {
		// Changed again since the stat: the events that follow say where to look.
		w.drop(rel)
		return nil
	}
	if err != nil {
		return err
	}
	n.info = info
	if sum != n.sum {
		old := n.sum
		n.sum = sum
		w.changes.add(rel, old, sum)
	}
	return nil
}

// drop removes rel and everything under it from the snapshot, reports its tracked files as
// removed, and gives back their system watches.
func (w *watcher) drop(rel string) {
	n := w.nodes[rel]
	if n == nil {
		return
	}
	for kid := range w.kids[rel] {
		w.drop(kid)
	}
	switch n.kind {
	case fileNode:
		w.unwatchFile(n)
		switch {
		case w.was != nil:
			w.was[rel] = n.sum
		case n.sum != "":
			w.changes.add(rel, n.sum, "")
		}
	case dirNode:
		w.dropWatch(n.real)
		delete(w.polled, rel)
		delete(w.dirty, rel)
		delete(w.owner, n.real)
		// A link elsewhere to the same directory may be the one to look into it now.
		for p, other := range w.nodes {
			if other.kind == aliasNode && other.real == n.real {
				w.dirty[path.Dir(p)] = true
			}
		}
	}
	delete(w.nodes, rel)
	delete(w.kids, rel)
	if rel != "." {
		parent := path.Dir(rel)
		delete(w.kids[parent], rel)
		if len(w.kids[parent]) == 0 {
			delete(w.kids, parent)
		}
	}
}

// renew runs look, which drops part of the snapshot and looks at it as new, and reports the
// changes against what it dropped: a file found again with the same content did not change.
func (w *watcher) renew(look func() error) error {
	if w.was != nil {
		return look() // within another renew
	}
	w.was = map[string]string{}
	defer func() { w.was = nil }()
	err := look()
	for rel, sum := range w.was {
		if sum != "" {
			w.changes.add(rel, sum, "")
		}
	}
	return err
}

// set puts n in the snapshot at rel.
func (w *watcher) set(rel string, n *node) {
	w.nodes[rel] = n
	if rel == "." {
		return
	}
	parent := path.Dir(rel)
	if w.kids[parent] == nil {
		w.kids[parent] = map[string]bool{}
	}
	w.kids[parent][rel] = true
}

// watchDir adds a system watch for the directory rel, whose real path is real. watched is false
// when rel has to be polled instead, because the watch would go past the budget or the system
// has no watch left.
func (w *watcher) watchDir(rel, real string) (watched bool, err error) {
	if w.full {
		return false, nil
	}
	if w.watchCount() >= w.budget {
		w.setFull()
		w.warnOnce(fmt.Sprintf("watching %s would use up the system's file watches, so it and the directories found after it are polled for changes", rel))
		return false, nil
	}
	err = w.add(real)
	if outOfWatches(err) {
		w.setFull()
		w.warnOnce(fmt.Sprintf("watching %s: %v: the system ran out of file watches, so it and the directories found after it are polled for changes", rel, err))
		w.spare()
		return false, nil
	}
	if err != nil {
		return false, err
	}
	w.watches[real] = true
	return true, nil
}

// watchFile adds a system watch for the file node n at rel where watchesFiles, unless its
// directory is polled. info is the file as check found it: a file replaced since its watch was
// added, as by an atomic save, gets a new one. A watch that would go past the budget polls the
// directory instead. Running out of file descriptors comes back as the error, see recover.
func (w *watcher) watchFile(rel string, n *node, info os.FileInfo) error {
	if !watchesFiles || w.sys == nil {
		return nil
	}
	dir := path.Dir(rel)
	if !w.isDir(dir) || w.polled[dir] {
		return nil
	}
	if n.watch != "" {
		if os.SameFile(n.info, info) {
			return nil
		}
		w.unwatchFile(n)
	}
	if w.watchCount() >= w.budget {
		w.pollDir(dir)
		w.warnOnce(fmt.Sprintf("%s grew past what the system's file watches allow, so it and the directories found after it are polled for changes", dir))
		return nil
	}
	name := filepath.Join(w.nodes[dir].real, path.Base(rel))
	err := w.add(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // gone again, the next event looks
	}
	if err != nil {
		return err
	}
	w.watches[name] = true
	n.watch = name
	return nil
}

// unwatchFile removes the system watch of the file node n, if it has one.
func (w *watcher) unwatchFile(n *node) {
	if n.watch != "" {
		w.dropWatch(n.watch)
		n.watch = ""
	}
}

// dropWatch removes the system watch of name, if it has one.
func (w *watcher) dropWatch(name string) {
	if w.watches[name] {
		_ = w.remove(name) // usually gone with the directory already
		delete(w.watches, name)
	}
}

// pollDir polls the directory node dir from now on, in place of its system watch and those of
// its files, and polls the directories found after it.
func (w *watcher) pollDir(dir string) {
	w.setFull()
	w.dropWatch(w.nodes[dir].real)
	for kid := range w.kids[dir] {
		w.unwatchFile(w.nodes[kid])
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
	for real, dir := range w.owner {
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
	for _, dir := range w.owner {
		w.polled[dir] = true
	}
	clear(w.watches)
	for _, n := range w.nodes {
		n.watch = ""
	}
	w.warnOnce(fmt.Sprintf("watching files: %v: the system ran out of file watches, so every directory is polled for changes", err))
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

// handle looks at the path an event names. A file or directory that an event says is gone or
// moved is dropped first: whatever is there now is new, as a new one can reuse the inode of the
// old one.
func (w *watcher) handle(name string, op fsnotify.Op) error {
	rel, ok := w.relOf(name)
	if !ok || !(rel == "." || w.isDir(path.Dir(rel))) {
		return nil // under a directory that is gone or not looked into
	}
	look := func() error { return w.look(rel, hinted) }
	if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
		look = func() error {
			w.drop(rel)
			return w.look(rel, hinted)
		}
	}
	if err := w.renew(look); err != nil {
		return err
	}
	if w.isDir(rel) {
		w.dirty[rel] = true // its entries changed, listed by run, see listDirty
	}
	return nil
}

// relOf maps an event path to a path under root. Directories are watched by their real path,
// so events name real paths: a link to a directory is known by the link's path under root.
func (w *watcher) relOf(name string) (string, bool) {
	if rel, ok := w.owner[name]; ok {
		return rel, true
	}
	if dir, ok := w.owner[filepath.Dir(name)]; ok {
		return path.Join(dir, filepath.Base(name)), true
	}
	if r, err := filepath.Rel(w.root, name); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(r), true
	}
	return "", false
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

// warnOnce warns with msg, unless it did with the same text before.
func (w *watcher) warnOnce(msg string) {
	if !w.warned[msg] {
		w.warned[msg] = true
		w.warn(msg)
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
