package devserver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
	w, err := newWatcher(dir, ignore)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return w
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
