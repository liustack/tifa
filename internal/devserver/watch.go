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

// changes collects the files whose content changed since the last batch was taken.
// The watcher adds to it, the rebuild loop takes batches from it.
type changes struct {
	mu       sync.Mutex
	before   map[string]string // checksum when the batch started, "" when the file did not exist
	after    map[string]string // checksum now, "" when the file is gone
	building bool              // a build runs and may read any file at any moment
	touched  map[string]bool   // files that changed while a build ran
	ready    chan struct{}     // receives a value after add, holds at most one
}

func newChanges() *changes {
	return &changes{before: map[string]string{}, after: map[string]string{}, touched: map[string]bool{}, ready: make(chan struct{}, 1)}
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
	c.mu.Lock()
	if _, ok := c.before[path]; !ok {
		c.before[path] = before
	}
	c.after[path] = after
	if c.building {
		c.touched[path] = true
	}
	c.mu.Unlock()
	select {
	case c.ready <- struct{}{}:
	default:
	}
}

// take returns the batch sorted by path and starts a new one. A file whose content ends up
// as it was when the batch started is left out, unless it changed while a build ran.
func (c *changes) take() []change {
	c.mu.Lock()
	defer c.mu.Unlock()
	var batch []change
	for p, before := range c.before {
		after := c.after[p]
		switch {
		case before == after && c.touched[p] && after == "":
			batch = append(batch, change{p, removed})
		case before == after && c.touched[p]:
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
	clear(c.before)
	clear(c.after)
	clear(c.touched)
	slices.SortFunc(batch, func(a, b change) int { return strings.Compare(a.path, b.path) })
	return batch
}

// skippedDirs are never watched, wherever they appear.
var skippedDirs = []string{".git", ".tifa", "node_modules"}

// watcher watches every directory under root and reports the files that plan cares about
// whose content changed. Paths in its maps are relative to root with forward slashes, the
// root itself is ".". Only run touches the maps once newWatcher returns.
type watcher struct {
	root     string
	rootReal string   // root with its links resolved
	ignore   []string // relative directories that are not watched, such as the build output
	fs       *fsnotify.Watcher
	files    map[string]string      // checksum of every tracked file
	dirs     map[string]os.FileInfo // watched directories, as they were when the watch was added
	real     map[string]string      // real path of each watched directory to its path under root
	linkDirs map[string]string      // watched directories that are links themselves, to the real path watched
	warn     func(string)
	warned   map[string]bool     // paths warned about, to warn once
	links    map[string][]string // real path of a linked file's target to the links under root
	linkOf   map[string]string   // link under root to the real path of its target
	extDirs  map[string]int      // directories watched for link targets, with how many links need each
	changes  *changes
}

// newWatcher watches root and records the checksum of every tracked file, without reporting them.
// ignore lists directories relative to root that are skipped. warn receives problems that do not
// stop watching, such as a symbolic link loop.
func newWatcher(root string, ignore []string, warn func(string)) (*watcher, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &watcher{
		root: abs, fs: fw, warn: warn, changes: newChanges(),
		files: map[string]string{}, dirs: map[string]os.FileInfo{}, real: map[string]string{}, linkDirs: map[string]string{}, warned: map[string]bool{},
		links: map[string][]string{}, linkOf: map[string]string{}, extDirs: map[string]int{},
	}
	if w.rootReal, err = filepath.EvalSymlinks(abs); err != nil {
		fw.Close()
		return nil, err
	}
	for _, d := range ignore {
		w.ignore = append(w.ignore, path.Clean(filepath.ToSlash(d)))
	}
	if err := w.rescan("."); err != nil {
		fw.Close()
		return nil, w.explain(err)
	}
	w.changes.take()
	return w, nil
}

// linkPoll is how often the watcher looks at the links it follows. kqueue (macOS, BSD) watches
// what a link leads to, so removing or replacing the link itself gives no event.
const linkPoll = 500 * time.Millisecond

// testHookLinkPoll lets tests see each path the link poll looks at.
var testHookLinkPoll func(rel string)

// run handles file events until ctx is done, then stops watching.
func (w *watcher) run(ctx context.Context) error {
	defer w.fs.Close()
	poll := time.NewTicker(linkPoll)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
			if err := w.checkLinks(); err != nil {
				return w.explain(err)
			}
		case ev, ok := <-w.fs.Events:
			if !ok {
				return errors.New("file watcher stopped")
			}
			if err := w.handle(ev.Name, ev.Op); err != nil {
				return w.explain(err)
			}
		case err, ok := <-w.fs.Errors:
			if !ok {
				return errors.New("file watcher stopped")
			}
			if !errors.Is(err, fsnotify.ErrEventOverflow) {
				return fmt.Errorf("watching files: %w", err)
			}
			// Some events were dropped, so compare everything again.
			if err := w.rescan("."); err != nil {
				return w.explain(err)
			}
		}
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
		// Whatever is at rel now, the watch for the old directory is gone. A new directory can
		// reuse the old one's inode, so identity alone cannot tell.
		w.unwatch(rel)
	}
	info, err := os.Stat(w.abs(rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		w.removeTree(rel)
		return nil
	case err != nil:
		return err
	case info.IsDir():
		return w.scan(rel, false)
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
	looped := false
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
		// Watch the real path: kqueue does not watch the files of a directory added through a link.
		err = w.fs.Add(real)
		if tooManyFiles(err) && raiseFileLimit() {
			err = w.fs.Add(real)
		}
		if err != nil {
			switch {
			case errors.Is(err, fs.ErrNotExist):
				w.removeTree(dir)
				return nil
			case errors.Is(err, syscall.ELOOP):
				// kqueue opens every entry of the directory and gives up at a link that loops.
				// The directory is watched, its files are added one by one below, but kqueue
				// may miss files created in it until the link is gone.
				w.warnOnce(dir, fmt.Sprintf("watching %s: %v: remove the looping link, until then new files in %s may go unnoticed", dir, err, dir))
				looped = true
			default:
				return fmt.Errorf("watching %s: %w", dir, err)
			}
		}
		w.dirs[dir] = info
		w.real[real] = dir
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
		if looped && kindOf(rel) != other {
			if err := w.fs.Add(w.abs(rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				w.warnOnce(rel, fmt.Sprintf("watching %s: %v", rel, err))
			}
		}
		if _, known := w.files[rel]; known && !deep {
			continue
		}
		if err := w.check(rel); err != nil {
			return err
		}
	}
	// Remove what is no longer in dir, at any depth: a vanished subdirectory takes its files along.
	gone := func(p string) (string, bool) {
		if p == dir || !under(dir, p) {
			return "", false
		}
		rest := strings.TrimPrefix(p, dir+"/")
		if dir == "." {
			rest = p
		}
		first, _, _ := strings.Cut(rest, "/")
		child := path.Join(dir, first)
		return child, !present[child]
	}
	for p := range w.files {
		if child, ok := gone(p); ok {
			w.removeTree(child)
		}
	}
	for d := range w.dirs {
		if child, ok := gone(d); ok {
			w.removeTree(child)
		}
	}
	return nil
}

func tooManyFiles(err error) bool {
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE)
}

// explain adds what to do when watching ran out of a system resource. kqueue (macOS, BSD) keeps a
// file descriptor open for every entry of a watched directory, inotify (Linux) has a limit on
// watched directories.
func (w *watcher) explain(err error) error {
	switch {
	case tooManyFiles(err):
		return fmt.Errorf("%w\nWatching %d directories needs a file descriptor for every file in them. Raise the limit with ulimit -n (now %d), or add directories whose files need no rebuild, such as large asset folders, to Ignore", err, len(w.dirs), fileLimit())
	case errors.Is(err, syscall.ENOSPC):
		return fmt.Errorf("%w\nThe system limit on watched directories is reached. Raise fs.inotify.max_user_watches, or add directories whose files need no rebuild to Ignore", err)
	}
	return err
}

// unwatch forgets the watch of dir, not of its subdirectories.
func (w *watcher) unwatch(dir string) {
	delete(w.dirs, dir)
	delete(w.linkDirs, dir)
	for real, owner := range w.real {
		if owner == dir {
			delete(w.real, real)
			_ = w.fs.Remove(real) // usually gone with the directory already
		}
	}
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
	sum, err := checksum(w.abs(rel))
	if errors.Is(err, fs.ErrNotExist) {
		w.removeTree(rel)
		return nil
	}
	if err != nil {
		return err
	}
	if old := w.files[rel]; sum != old {
		w.files[rel] = sum
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
	dir := filepath.Dir(target)
	if _, watched := w.real[dir]; watched {
		return nil
	}
	if w.extDirs[dir] == 0 {
		if err := w.fs.Add(dir); err != nil {
			w.warnOnce(rel, fmt.Sprintf("edits to %s, the target of %s, may go unnoticed: %v", target, rel, err))
		}
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
			_ = w.fs.Remove(dir)
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
			w.unlink(p)
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
