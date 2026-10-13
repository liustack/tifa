package devserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

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
	root    string
	ignore  []string // relative directories that are not watched, such as the build output
	fs      *fsnotify.Watcher
	files   map[string]string      // checksum of every tracked file
	dirs    map[string]os.FileInfo // watched directories, as they were when the watch was added
	changes *changes
}

// newWatcher watches root and records the checksum of every tracked file, without reporting them.
// ignore lists directories relative to root that are skipped.
func newWatcher(root string, ignore []string) (*watcher, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &watcher{root: abs, fs: fw, files: map[string]string{}, dirs: map[string]os.FileInfo{}, changes: newChanges()}
	for _, d := range ignore {
		w.ignore = append(w.ignore, path.Clean(filepath.ToSlash(d)))
	}
	if err := w.rescan("."); err != nil {
		fw.Close()
		return nil, err
	}
	w.changes.take()
	return w, nil
}

// run handles file events until ctx is done, then stops watching.
func (w *watcher) run(ctx context.Context) error {
	defer w.fs.Close()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.fs.Events:
			if !ok {
				return errors.New("file watcher stopped")
			}
			if err := w.handle(ev.Name, ev.Op); err != nil {
				return err
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
				return err
			}
		}
	}
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
	r, err := filepath.Rel(w.root, name)
	if err != nil {
		return err
	}
	rel := filepath.ToSlash(r)
	if rel == ".." || strings.HasPrefix(rel, "../") || w.skipped(rel) {
		return nil
	}
	if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
		// Whatever is at rel now, the watch for the old directory is gone. A new directory can
		// reuse the old one's inode, so identity alone cannot tell.
		w.unwatch(rel)
	}
	info, err := os.Stat(name)
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
		_ = w.fs.Remove(w.abs(dir)) // a replaced directory may still have the old watch
		if err := w.fs.Add(w.abs(dir)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				w.removeTree(dir)
				return nil
			}
			return fmt.Errorf("watching %s: %w", dir, err)
		}
		w.dirs[dir] = info
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

// unwatch forgets the watch of dir, not of its subdirectories.
func (w *watcher) unwatch(dir string) {
	if _, ok := w.dirs[dir]; ok {
		delete(w.dirs, dir)
		_ = w.fs.Remove(w.abs(dir)) // usually gone with the directory already
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

// removeTree forgets rel and everything under it, reporting the tracked files as removed.
func (w *watcher) removeTree(rel string) {
	for p, sum := range w.files {
		if under(rel, p) {
			delete(w.files, p)
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
