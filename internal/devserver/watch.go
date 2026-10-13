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
	"time"

	"github.com/fsnotify/fsnotify"
)

// changes collects the files whose content changed since the last batch was taken.
// The watcher adds to it, the rebuild loop takes batches from it.
type changes struct {
	mu     sync.Mutex
	before map[string]string // checksum when the batch started, "" when the file did not exist
	after  map[string]string // checksum now, "" when the file is gone
	ready  chan struct{}     // receives a value after add, holds at most one
}

func newChanges() *changes {
	return &changes{before: map[string]string{}, after: map[string]string{}, ready: make(chan struct{}, 1)}
}

// add records that path went from checksum before to checksum after.
func (c *changes) add(path, before, after string) {
	c.mu.Lock()
	if _, ok := c.before[path]; !ok {
		c.before[path] = before
	}
	c.after[path] = after
	c.mu.Unlock()
	select {
	case c.ready <- struct{}{}:
	default:
	}
}

// take returns the batch sorted by path and starts a new one. A file whose content ends up
// as it was when the batch started is left out.
func (c *changes) take() []change {
	c.mu.Lock()
	defer c.mu.Unlock()
	var batch []change
	for p, before := range c.before {
		after := c.after[p]
		switch {
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
	slices.SortFunc(batch, func(a, b change) int { return strings.Compare(a.path, b.path) })
	return batch
}

// skippedDirs are never watched, wherever they appear.
var skippedDirs = []string{".git", ".tifa", "node_modules"}

type file struct {
	size int64
	mod  time.Time
	sum  string
}

// watcher watches every directory under root and reports the files that plan cares about
// whose content changed. Paths in its maps are relative to root with forward slashes, the
// root itself is ".". Only run touches the maps once newWatcher returns.
type watcher struct {
	root    string
	ignore  []string // relative directories that are not watched, such as the build output
	fs      *fsnotify.Watcher
	files   map[string]file
	dirs    map[string]bool
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
	w := &watcher{root: abs, fs: fw, files: map[string]file{}, dirs: map[string]bool{}, changes: newChanges()}
	for _, d := range ignore {
		w.ignore = append(w.ignore, path.Clean(filepath.ToSlash(d)))
	}
	if err := w.scanDir("."); err != nil {
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
			if err := w.handle(ev.Name); err != nil {
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
			if err := w.scanDir("."); err != nil {
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

// handle brings the state of one path up to date. Events only say where to look: the checksum
// decides whether a file changed.
func (w *watcher) handle(name string) error {
	r, err := filepath.Rel(w.root, name)
	if err != nil {
		return err
	}
	rel := filepath.ToSlash(r)
	if rel == ".." || strings.HasPrefix(rel, "../") || w.skipped(rel) {
		return nil
	}
	info, err := os.Stat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		w.removeTree(rel)
		return nil
	case err != nil:
		return err
	case info.IsDir():
		return w.scanDir(rel)
	default:
		return w.check(rel, info)
	}
}

// scanDir watches dir, checks every file in it and scans subdirectories that are not watched yet.
// Files and directories that disappeared from dir count as removed.
func (w *watcher) scanDir(dir string) error {
	if !w.dirs[dir] {
		if err := w.fs.Add(w.abs(dir)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				w.removeTree(dir)
				return nil
			}
			return fmt.Errorf("watching %s: %w", dir, err)
		}
		w.dirs[dir] = true
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
		present[rel] = true
		info, err := os.Stat(w.abs(rel)) // follows symlinks, like the build does
		if errors.Is(err, fs.ErrNotExist) {
			delete(present, rel)
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			if !w.dirs[rel] {
				if err := w.scanDir(rel); err != nil {
					return err
				}
			}
			continue
		}
		if err := w.check(rel, info); err != nil {
			return err
		}
	}
	for rel := range w.files {
		if path.Dir(rel) == dir && !present[rel] {
			w.removeTree(rel)
		}
	}
	for rel := range w.dirs {
		if rel != dir && path.Dir(rel) == dir && !present[rel] {
			w.removeTree(rel)
		}
	}
	return nil
}

// check rehashes a tracked file when its size or modification time changed.
func (w *watcher) check(rel string, info fs.FileInfo) error {
	if kindOf(rel) == other || !info.Mode().IsRegular() {
		return nil
	}
	old, known := w.files[rel]
	if known && old.size == info.Size() && old.mod.Equal(info.ModTime()) {
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
	w.files[rel] = file{info.Size(), info.ModTime(), sum}
	if sum != old.sum {
		w.changes.add(rel, old.sum, sum)
	}
	return nil
}

// removeTree forgets rel and everything under it, reporting the tracked files as removed.
func (w *watcher) removeTree(rel string) {
	under := func(p string) bool { return rel == "." || p == rel || strings.HasPrefix(p, rel+"/") }
	for p, f := range w.files {
		if under(p) {
			delete(w.files, p)
			w.changes.add(p, f.sum, "")
		}
	}
	for d := range w.dirs {
		if under(d) {
			delete(w.dirs, d)
			_ = w.fs.Remove(w.abs(d)) // the watch is usually gone with the directory already
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
