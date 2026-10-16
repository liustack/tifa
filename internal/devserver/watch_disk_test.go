package devserver

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// The watcher's events only say where to look: what it reports follows from what is on the disk
// when it looks, whatever it saw there before.

// seeChanges collects batches until they hold everything in want. Other changes may come along.
func seeChanges(t *testing.T, w *watcher, want ...change) {
	t.Helper()
	var got []change
	timeout := time.After(5 * time.Second)
	for !containsAll(got, want) {
		select {
		case <-w.changes.ready:
			got = append(got, w.changes.take()...)
		case <-timeout:
			t.Fatalf("got %v within 5s, want %v among them", got, want)
		}
	}
}

// A link to a directory another link already covers takes over when that link is pointed
// elsewhere.
func TestWatcherTakesOverFromARetargetedLink(t *testing.T) {
	dir, one, two := t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, one, "x.ts", "one")
	writeFile(t, two, "x.ts", "two")
	writeFile(t, dir, "marker.ts", "start")
	symlink(t, one, filepath.Join(dir, "a"))
	symlink(t, one, filepath.Join(dir, "z"))
	w := startWatcher(t, dir)
	symlink(t, two, filepath.Join(dir, "a.new"))
	if err := os.Rename(filepath.Join(dir, "a.new"), filepath.Join(dir, "a")); err != nil {
		t.Fatal(err)
	}
	seeChanges(t, w, change{"z/x.ts", created})
	writeFile(t, one, "x.ts", "one edited")
	seeChanges(t, w, change{"z/x.ts", modified})
}

// A link to a directory another link already covers takes over when that link is removed.
func TestWatcherTakesOverFromARemovedLink(t *testing.T) {
	dir, shared := t.TempDir(), t.TempDir()
	writeFile(t, shared, "x.ts", "one")
	writeFile(t, dir, "marker.ts", "start")
	symlink(t, shared, filepath.Join(dir, "a"))
	symlink(t, shared, filepath.Join(dir, "z"))
	w := startWatcher(t, dir)
	if err := os.Remove(filepath.Join(dir, "a")); err != nil {
		t.Fatal(err)
	}
	seeChanges(t, w, change{"a/x.ts", removed}, change{"z/x.ts", created})
	writeFile(t, shared, "x.ts", "two")
	seeChanges(t, w, change{"z/x.ts", modified})
}

// A file that becomes a directory is removed, and the files in the directory are new, also when
// the listing of its parent comes before the event about the file.
func TestWatcherSeesAFileBecomeADirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.ts", "old")
	w := preparedWatcher(t, dir, func(msg string) { t.Log(msg) })
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "a.ts")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "a.ts/child.ts", "new")
	if err := w.handle(w.rootReal, fsnotify.Write); err != nil {
		t.Fatal(err)
	}
	if err := w.listDirty(); err != nil {
		t.Fatal(err)
	}
	if err := w.handle(filepath.Join(w.rootReal, "a.ts"), fsnotify.Remove); err != nil {
		t.Fatal(err)
	}
	got := w.changes.take()
	if want := []change{{"a.ts", removed}, {"a.ts/child.ts", created}}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A directory that becomes a file takes its files along. An event about one of them that comes
// after the change finds a file where its directory was, which is no error.
func TestWatcherSeesADirectoryBecomeAFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.ts/child.ts", "old")
	w := preparedWatcher(t, dir, func(msg string) { t.Log(msg) })
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "a.ts")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "a.ts", "new")
	for _, ev := range []fsnotify.Event{
		{Name: filepath.Join(w.rootReal, "a.ts", "child.ts"), Op: fsnotify.Write},
		{Name: filepath.Join(w.rootReal, "a.ts"), Op: fsnotify.Remove},
		{Name: w.rootReal, Op: fsnotify.Write},
	} {
		if err := w.handle(ev.Name, ev.Op); err != nil {
			t.Fatalf("%s: %v", ev, err)
		}
	}
	if err := w.listDirty(); err != nil {
		t.Fatal(err)
	}
	got := w.changes.take()
	if want := []change{{"a.ts", created}, {"a.ts/child.ts", removed}}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A link to a file outside the root whose target goes away and comes back is found again.
func TestWatcherFindsALinkTargetThatComesBack(t *testing.T) {
	dir, shared := t.TempDir(), t.TempDir()
	writeFile(t, shared, "value.ts", "one")
	symlink(t, filepath.Join(shared, "value.ts"), filepath.Join(dir, "link.ts"))
	w := startWatcher(t, dir)
	if err := os.Remove(filepath.Join(shared, "value.ts")); err != nil {
		t.Fatal(err)
	}
	seeChanges(t, w, change{"link.ts", removed})
	writeFile(t, shared, "value.ts", "two")
	seeChanges(t, w, change{"link.ts", created})
}

// A directory reached first through a link elsewhere is looked into at its own path once that
// link is gone, though nothing changed in the directory that holds it.
func TestWatcherTakesOverAtTheDirectoryItself(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "lib/real/x.ts", "one")
	writeFile(t, dir, "marker.ts", "start")
	symlink(t, filepath.Join(dir, "lib/real"), filepath.Join(dir, "a"))
	w := startWatcher(t, dir)
	if err := os.Remove(filepath.Join(dir, "a")); err != nil {
		t.Fatal(err)
	}
	seeChanges(t, w, change{"a/x.ts", removed}, change{"lib/real/x.ts", created})
	writeFile(t, dir, "lib/real/x.ts", "two")
	seeChanges(t, w, change{"lib/real/x.ts", modified})
}
