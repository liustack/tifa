package devserver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestChangesMergeByPath(t *testing.T) {
	c := newChanges()
	c.add("a.go", "s1", "s2")
	c.add("a.go", "s2", "s1") // edited back before the batch was taken
	c.add("b.tsx", "", "s3")
	c.add("b.tsx", "s3", "") // created and removed again
	c.add("c.tsx", "", "s4")
	c.add("c.tsx", "s4", "s5")
	c.add("d.css", "s6", "")
	c.add("e.ts", "s7", "s8")
	c.add("e.ts", "s8", "s9")
	want := []change{{"c.tsx", created}, {"d.css", removed}, {"e.ts", modified}}
	if got := c.take(); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := c.take(); len(got) != 0 {
		t.Fatalf("second take got %v, want nothing", got)
	}
}

// startWatcher watches dir until the test ends.
func startWatcher(t *testing.T, dir string, ignore ...string) *watcher {
	t.Helper()
	w, err := newWatcher(dir, ignore, func(msg string) { t.Log(msg) })
	if err != nil {
		t.Fatal(err)
	}
	runWatcher(t, w)
	return w
}

// runWatcher runs w until the test ends.
func runWatcher(t *testing.T, w *watcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
}

// nextBatch waits for the next non-empty batch.
func nextBatch(t *testing.T, c *changes) []change {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-c.ready:
			if batch := c.take(); len(batch) > 0 {
				return batch
			}
		case <-timeout:
			t.Fatal("no change reported within 5s")
		}
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// expectBatch writes the marker file, then collects batches until the marker shows up and checks
// that they hold exactly want plus the marker. Events arrive in order, so anything reported for
// earlier writes is collected by then.
func expectBatch(t *testing.T, dir string, w *watcher, want ...change) {
	t.Helper()
	writeFile(t, dir, "marker.ts", time.Now().String())
	marker := change{"marker.ts", modified}
	want = append(want, marker)
	var got []change
	for !slices.Contains(got, marker) {
		got = append(got, nextBatch(t, w.changes)...)
	}
	sortChanges(want)
	sortChanges(got)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func sortChanges(c []change) {
	slices.SortFunc(c, func(a, b change) int { return strings.Compare(a.path, b.path) })
}

func newApp(t *testing.T) string {
	dir := t.TempDir()
	writeFile(t, dir, "marker.ts", "start")
	writeFile(t, dir, "app/store.go", "package app")
	writeFile(t, dir, "app/list/route.tsx", "export {}")
	writeFile(t, dir, "node_modules/react/index.js", "x")
	writeFile(t, dir, ".tifa/main.go", "package main")
	writeFile(t, dir, "bin/app.js", "x")
	return dir
}

func TestWatcherSkipsUnchangedContent(t *testing.T) {
	dir := newApp(t)
	w := startWatcher(t, dir)
	writeFile(t, dir, "app/store.go", "package app") // saved without edits
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "app/list/route.tsx"), later, later); err != nil {
		t.Fatal(err)
	}
	expectBatch(t, dir, w)
}

func TestWatcherReportsEdits(t *testing.T) {
	dir := newApp(t)
	w := startWatcher(t, dir)
	writeFile(t, dir, "app/store.go", "package app // edited")
	expectBatch(t, dir, w, change{"app/store.go", modified})
}

func TestWatcherReportsCreateAndRemove(t *testing.T) {
	dir := newApp(t)
	w := startWatcher(t, dir)
	writeFile(t, dir, "app/list/items.tsx", "export {}")
	expectBatch(t, dir, w, change{"app/list/items.tsx", created})
	if err := os.Remove(filepath.Join(dir, "app/list/items.tsx")); err != nil {
		t.Fatal(err)
	}
	expectBatch(t, dir, w, change{"app/list/items.tsx", removed})
}

func TestWatcherReportsRename(t *testing.T) {
	dir := newApp(t)
	w := startWatcher(t, dir)
	if err := os.Rename(filepath.Join(dir, "app/list/route.tsx"), filepath.Join(dir, "app/list/page.tsx")); err != nil {
		t.Fatal(err)
	}
	expectBatch(t, dir, w, change{"app/list/page.tsx", created}, change{"app/list/route.tsx", removed})
}

func TestWatcherFollowsNewDirectories(t *testing.T) {
	dir := newApp(t)
	w := startWatcher(t, dir)
	// The file may be written before the watcher sees the new directories, it still counts.
	writeFile(t, dir, "app/orders/detail/route.tsx", "export {}")
	expectBatch(t, dir, w, change{"app/orders/detail/route.tsx", created})
	// The new directory is watched from now on.
	writeFile(t, dir, "app/orders/detail/route.go", "package detail")
	expectBatch(t, dir, w, change{"app/orders/detail/route.go", created})
}

func TestWatcherReportsRemovedDirectories(t *testing.T) {
	dir := newApp(t)
	w := startWatcher(t, dir)
	if err := os.RemoveAll(filepath.Join(dir, "app/list")); err != nil {
		t.Fatal(err)
	}
	expectBatch(t, dir, w, change{"app/list/route.tsx", removed})
}

func TestWatcherIgnoresOutputAndOtherFiles(t *testing.T) {
	dir := newApp(t)
	w := startWatcher(t, dir, "bin")
	writeFile(t, dir, "node_modules/react/index.js", "y")
	writeFile(t, dir, "node_modules/react/new.js", "y")
	writeFile(t, dir, ".tifa/main.go", "package main // new")
	writeFile(t, dir, ".tifa/routes/routes.go", "package routes")
	writeFile(t, dir, "bin/app.js", "y")
	writeFile(t, dir, "README.md", "# app")
	writeFile(t, dir, "app/logo.png", "png")
	expectBatch(t, dir, w)
}

// An atomic save can keep the size and modification time while the content differs.
func TestWatcherHashesOnEveryEvent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tsx", "AAA")
	w, err := newWatcher(dir, nil, func(msg string) { t.Log(msg) })
	if err != nil {
		t.Fatal(err)
	}
	defer w.fs.Close()
	info, err := os.Stat(filepath.Join(dir, "a.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "a.tsx")
	if err := os.WriteFile(tmp, []byte("BBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "a.tsx")); err != nil {
		t.Fatal(err)
	}
	if err := w.handle(filepath.Join(dir, "a.tsx"), fsnotify.Create); err != nil {
		t.Fatal(err)
	}
	if got := w.changes.take(); !slices.Equal(got, []change{{"a.tsx", modified}}) {
		t.Fatalf("got %v, want a.tsx modified", got)
	}
}

// A directory deleted and created again before the watcher handles the events gets a new watch.
func TestWatcherRewatchesARecreatedDirectory(t *testing.T) {
	dir := newApp(t)
	w, err := newWatcher(dir, nil, func(msg string) { t.Log(msg) })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "app")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "app/store.go", "package app")
	writeFile(t, dir, "app/list/route.tsx", "export {}")
	runWatcher(t, w) // handles the queued events now
	expectBatch(t, dir, w)
	writeFile(t, dir, "app/next.tsx", "export {}")
	writeFile(t, dir, "app/list/items.tsx", "export {}")
	expectBatch(t, dir, w, change{"app/list/items.tsx", created}, change{"app/next.tsx", created})
}

// After dropped events the watcher compares every file again, in known directories too.
func TestWatcherRescanFindsEverything(t *testing.T) {
	dir := newApp(t)
	w, err := newWatcher(dir, nil, func(msg string) { t.Log(msg) })
	if err != nil {
		t.Fatal(err)
	}
	defer w.fs.Close()
	writeFile(t, dir, "app/store.go", "package app // edited")
	writeFile(t, dir, "app/list/route.tsx", "export const edited = 1")
	writeFile(t, dir, "app/deep/er/route.tsx", "export {}")
	if err := os.Remove(filepath.Join(dir, "marker.ts")); err != nil {
		t.Fatal(err)
	}
	if err := w.rescan("."); err != nil {
		t.Fatal(err)
	}
	want := []change{
		{"app/deep/er/route.tsx", created},
		{"app/list/route.tsx", modified},
		{"app/store.go", modified},
		{"marker.ts", removed},
	}
	if got := w.changes.take(); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// warnings collects what the watcher warns about.
type warnings struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnings) add(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, msg)
}

func (w *warnings) has(t *testing.T, substr string) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, m := range w.msgs {
		if strings.Contains(m, substr) {
			return
		}
	}
	t.Fatalf("no warning mentions %q, got %q", substr, w.msgs)
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// Links that lead back into a watched directory are skipped with a warning, not followed forever.
func TestWatcherSkipsSymlinkCycles(t *testing.T) {
	dir := newApp(t)
	symlink(t, dir, filepath.Join(dir, "app/root"))
	symlink(t, "self", filepath.Join(dir, "app/self"))
	var warned warnings
	w, err := newWatcher(dir, nil, warned.add)
	if err != nil {
		t.Fatal(err)
	}
	runWatcher(t, w)
	warned.has(t, "app/root")
	warned.has(t, "app/self")
	// The same while running.
	symlink(t, filepath.Join(dir, "app"), filepath.Join(dir, "app/list/up"))
	writeFile(t, dir, "app/store.go", "package app // edited")
	expectBatch(t, dir, w, change{"app/store.go", modified})
	warned.has(t, "app/list/up")
}

// Two links to one directory are watched once.
func TestWatcherWatchesALinkedDirectoryOnce(t *testing.T) {
	dir, shared := newApp(t), t.TempDir()
	writeFile(t, shared, "button.tsx", "export {}")
	symlink(t, shared, filepath.Join(dir, "app/a"))
	symlink(t, shared, filepath.Join(dir, "app/b"))
	var warned warnings
	w, err := newWatcher(dir, nil, warned.add)
	if err != nil {
		t.Fatal(err)
	}
	runWatcher(t, w)
	warned.has(t, "app/b")
	writeFile(t, shared, "button.tsx", "export const edited = 1")
	expectBatch(t, dir, w, change{"app/a/button.tsx", modified})
}

// A file linked from outside the app reports edits made to its target, also after an atomic save.
// inotify reports such edits only to the target's directory, so that directory is watched too.
func TestWatcherFollowsLinkedFiles(t *testing.T) {
	dir, shared := newApp(t), t.TempDir()
	writeFile(t, shared, "theme.css", "a {}")
	symlink(t, filepath.Join(shared, "theme.css"), filepath.Join(dir, "app/theme.css"))
	symlink(t, filepath.Join(dir, "app/store.go"), filepath.Join(dir, "app/alias.go"))
	w := startWatcher(t, dir)

	writeFile(t, shared, "tmp", "b {}")
	if err := os.Rename(filepath.Join(shared, "tmp"), filepath.Join(shared, "theme.css")); err != nil {
		t.Fatal(err)
	}
	expectBatch(t, dir, w, change{"app/theme.css", modified})
	writeFile(t, shared, "theme.css", "c {}")
	expectBatch(t, dir, w, change{"app/theme.css", modified})

	writeFile(t, dir, "app/store.go", "package app // edited")
	expectBatch(t, dir, w, change{"app/alias.go", modified}, change{"app/store.go", modified})
}

// Removing a link reports its files as removed while the target stays. kqueue (macOS) gives no
// event for that: it watches the target the link leads to.
func TestWatcherReportsRemovedLinks(t *testing.T) {
	dir, shared := newApp(t), t.TempDir()
	writeFile(t, shared, "classes.ts", "export const c = 'text-pink-700'")
	writeFile(t, shared, "kit/button.tsx", "export {}")
	symlink(t, filepath.Join(shared, "classes.ts"), filepath.Join(dir, "app/classes.ts"))
	symlink(t, filepath.Join(shared, "kit"), filepath.Join(dir, "app/kit"))
	w := startWatcher(t, dir)

	for _, link := range []string{"app/classes.ts", "app/kit"} {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(link))); err != nil {
			t.Fatal(err)
		}
	}
	want := []change{{"app/classes.ts", removed}, {"app/kit/button.tsx", removed}}
	var got []change
	for len(got) < len(want) {
		got = append(got, nextBatch(t, w.changes)...)
	}
	sortChanges(got)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// The targets are no longer watched for the app.
	writeFile(t, shared, "classes.ts", "export const c = 1")
	writeFile(t, shared, "kit/button.tsx", "export const b = 1")
	expectBatch(t, dir, w)
}
