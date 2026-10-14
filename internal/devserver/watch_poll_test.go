package devserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// outOfWatchesErrors returns the errors this system reports when it runs out of watches.
func outOfWatchesErrors(t *testing.T) []error {
	t.Helper()
	var errs []error
	for _, err := range []error{syscall.EMFILE, syscall.ENFILE, syscall.ENOSPC} {
		if outOfWatches(err) {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		t.Skip("this system reports no error for running out of watches")
	}
	return errs
}

// preparedWatcher returns a watcher of dir that has not scanned yet, so a test can change how it
// adds watches. It is closed when the test ends.
func preparedWatcher(t *testing.T, dir string, warn func(string)) *watcher {
	t.Helper()
	w, err := prepareWatcher(dir, nil, warn)
	if err != nil {
		t.Fatal(err)
	}
	w.minPoll = 10 * time.Millisecond
	t.Cleanup(func() { w.close() })
	return w
}

// expectChanges collects batches until they hold everything in want, and checks they hold nothing
// else. Polled and watched directories report in no particular order, so no marker file is used.
func expectChanges(t *testing.T, w *watcher, want ...change) {
	t.Helper()
	var got []change
	for !containsAll(got, want) {
		got = append(got, nextBatch(t, w.changes)...)
	}
	sortChanges(want)
	sortChanges(got)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func containsAll(got, want []change) bool {
	for _, c := range want {
		if !slices.Contains(got, c) {
			return false
		}
	}
	return true
}

// failAfter makes adding a watch fail with err from the nth call on, as a system that ran out does.
func failAfter(w *watcher, n int, err error) *int {
	calls := 0
	add := w.add
	w.add = func(name string) error {
		calls++
		if calls >= n {
			return &os.PathError{Op: "open", Path: name, Err: err}
		}
		return add(name)
	}
	return &calls
}

// The directories that cannot be watched are polled, and every change in them is still reported:
// edits, new files, new directories and removals.
func TestWatcherPollsWhenOutOfWatches(t *testing.T) {
	for _, outErr := range outOfWatchesErrors(t) {
		t.Run(outErr.Error(), func(t *testing.T) {
			dir := newApp(t)
			writeFile(t, dir, "app/list/items/table.tsx", "export {}")
			var warned warnings
			w := preparedWatcher(t, dir, warned.add)
			calls := failAfter(w, 2, outErr) // the root is watched, app and below are not
			if err := w.start(); err != nil {
				t.Fatal(err)
			}
			for _, d := range []string{"app", "app/list", "app/list/items"} {
				if !w.polled[d] {
					t.Fatalf("%s is not polled, polled: %v", d, w.polled)
				}
			}
			// Running out also gives back up to spareWatches, here every watch.
			if w.used != 0 || len(w.cost) != 0 || !w.polled["."] {
				t.Fatalf("watches left after running out: %v", w.cost)
			}
			if *calls != 2 {
				t.Fatalf("adding watches was tried %d times, want 2: once out, the watcher stops trying", *calls)
			}
			warned.has(t, "app")
			if len(warned.msgs) != 1 {
				t.Fatalf("warned %d times, want once: %q", len(warned.msgs), warned.msgs)
			}
			runWatcher(t, w)

			writeFile(t, dir, "app/store.go", "package app // edited")
			expectChanges(t, w, change{"app/store.go", modified})
			writeFile(t, dir, "app/list/items/table.tsx", "export const edited = 1")
			expectChanges(t, w, change{"app/list/items/table.tsx", modified})
			writeFile(t, dir, "app/orders/detail/route.tsx", "export {}")
			expectChanges(t, w, change{"app/orders/detail/route.tsx", created})
			writeFile(t, dir, "app/orders/detail/route.tsx", "export const edited = 1")
			expectChanges(t, w, change{"app/orders/detail/route.tsx", modified})
			if err := os.RemoveAll(filepath.Join(dir, "app/list")); err != nil {
				t.Fatal(err)
			}
			expectChanges(t, w, change{"app/list/items/table.tsx", removed}, change{"app/list/route.tsx", removed})
			// A directory created under a watched one once the system ran out is polled too.
			writeFile(t, dir, "lib/format.ts", "export {}")
			expectChanges(t, w, change{"lib/format.ts", created})
			writeFile(t, dir, "lib/format.ts", "export const edited = 1")
			expectChanges(t, w, change{"lib/format.ts", modified})
		})
	}
}

// An atomic save that keeps the size and the modification time is still found in a polled
// directory: the saved file is a new file.
func TestWatcherPollFindsAtomicSaves(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/a.tsx", "AAA")
	w := preparedWatcher(t, dir, func(msg string) { t.Log(msg) })
	w.budget = 0
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "app/a.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "app/a.tsx.tmp")
	if err := os.WriteFile(tmp, []byte("BBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "app/a.tsx")); err != nil {
		t.Fatal(err)
	}
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
	if got := w.changes.take(); !slices.Equal(got, []change{{"app/a.tsx", modified}}) {
		t.Fatalf("got %v, want app/a.tsx modified", got)
	}
}

// A poll hashes only the files whose size, modification time or identity changed.
func TestWatcherPollHashesOnlyChangedFiles(t *testing.T) {
	dir := newApp(t)
	w := preparedWatcher(t, dir, func(msg string) { t.Log(msg) })
	w.budget = 0
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	var hashed []string
	testHookChecksum = func(rel string) { hashed = append(hashed, rel) }
	defer func() { testHookChecksum = nil }()
	writeFile(t, dir, "app/store.go", "package app // edited")
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(hashed, []string{"app/store.go"}) {
		t.Fatalf("hashed %v, want only app/store.go", hashed)
	}
}

// Watches that would go past the budget are not added: those directories are polled and still
// report their changes, the others keep their system watch.
func TestWatcherPollsBeyondTheBudget(t *testing.T) {
	dir := newApp(t)
	var warned warnings
	w := preparedWatcher(t, dir, warned.add)
	w.budget = watchCost(w.rootReal) // the root alone
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	if w.polled["."] || !w.polled["app"] || !w.polled["app/list"] {
		t.Fatalf("polled %v, want everything but the root", w.polled)
	}
	warned.has(t, "polled")
	runWatcher(t, w)
	writeFile(t, dir, "app/list/route.tsx", "export const edited = 1")
	writeFile(t, dir, "marker.ts", "edited")
	expectChanges(t, w, change{"app/list/route.tsx", modified}, change{"marker.ts", modified})
}

// Past a budget of nothing, nothing is added and everything is polled.
func TestWatcherPollsEverythingWithoutBudget(t *testing.T) {
	dir := newApp(t)
	w := preparedWatcher(t, dir, func(msg string) { t.Log(msg) })
	w.budget = 0
	calls := failAfter(w, 1, errors.New("no watch may be added"))
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatalf("adding watches was tried %d times past the budget", *calls)
	}
	runWatcher(t, w)
	writeFile(t, dir, "marker.ts", "edited")
	expectChanges(t, w, change{"marker.ts", modified})
}

// When the system runs out while the watcher runs, such as kqueue failing to open a new file in a
// watched directory, it cannot tell which directory missed events: it polls them all.
func TestWatcherPollsEverythingWhenRunningOut(t *testing.T) {
	outErr := outOfWatchesErrors(t)[0]
	dir := newApp(t)
	var warned warnings
	w := preparedWatcher(t, dir, warned.add)
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 1) // fsnotify owns and closes w.fs.Errors
	w.errs = errs
	runWatcher(t, w)
	errs <- fmt.Errorf("fsnotify.dirChange: %w", outErr)
	writeFile(t, dir, "app/list/route.tsx", "export const edited = 1")
	expectChanges(t, w, change{"app/list/route.tsx", modified})
	warned.has(t, "polled")
}

// Without any watch from the system, such as when inotify has no instance left, everything is
// polled from the start.
func TestWatcherPollsWithoutASystemWatcher(t *testing.T) {
	outErr := outOfWatchesErrors(t)[0]
	newFSWatcher = func() (*fsnotify.Watcher, error) { return nil, outErr }
	defer func() { newFSWatcher = fsnotify.NewWatcher }()
	dir := newApp(t)
	var warned warnings
	w, err := newWatcher(dir, nil, warned.add)
	if err != nil {
		t.Fatal(err)
	}
	w.minPoll = 10 * time.Millisecond
	warned.has(t, "polled")
	runWatcher(t, w)
	writeFile(t, dir, "app/orders/route.tsx", "export {}")
	expectChanges(t, w, change{"app/orders/route.tsx", created})
}

// The poll interval grows with the time a poll takes, so polling a large tree stays cheap.
func TestPollInterval(t *testing.T) {
	for _, c := range []struct{ took, want time.Duration }{
		{0, minPollInterval},
		{10 * time.Millisecond, minPollInterval},
		{200 * time.Millisecond, 200 * time.Millisecond * pollCostFactor},
	} {
		w := &watcher{minPoll: minPollInterval}
		if got := w.pollInterval(c.took); got != c.want {
			t.Errorf("pollInterval(%v) = %v, want %v", c.took, got, c.want)
		}
	}
}

// On kqueue every file created in a watched directory takes a file descriptor of its own. Once
// they go past the budget, the directory is polled instead, and its new files are still reported.
func TestWatcherPollsADirectoryThatGrowsPastTheBudget(t *testing.T) {
	if entryCost == 0 {
		t.Skip("a new file takes no watch of its own on this system")
	}
	dir := t.TempDir()
	writeFile(t, dir, "app/a.ts", "export {}")
	var warned warnings
	w := preparedWatcher(t, dir, warned.add)
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	w.budget = w.used + 5
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.run(ctx) }()
	var want []change
	for i := range 20 {
		name := fmt.Sprintf("app/f%02d.ts", i)
		writeFile(t, dir, name, "export {}")
		want = append(want, change{name, created})
	}
	expectChanges(t, w, want...)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if w.used > w.budget || !w.polled["app"] {
		t.Fatalf("used %d of a budget of %d, polled %v: the new files went past the budget", w.used, w.budget, w.polled)
	}
	warned.has(t, "app")
}

// kqueue reports new files by listing the directory, and fsnotify stops at the first file it
// cannot open when the process runs out of descriptors, without a word: the files after it get no
// event. The watcher lists a directory again once a new file shows up in it, so it finds them.
func TestWatcherFindsFilesWithoutEvents(t *testing.T) {
	if entryCost == 0 {
		t.Skip("the system reports every new file")
	}
	dir := t.TempDir()
	writeFile(t, dir, "app/a.ts", "export {}")
	w := preparedWatcher(t, dir, func(msg string) { t.Log(msg) })
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	events := make(chan fsnotify.Event)
	w.events = events // fsnotify's events go unread, as if it had stopped
	runWatcher(t, w)
	for _, name := range []string{"app/b.ts", "app/c.ts", "app/d.ts"} {
		writeFile(t, dir, name, "export {}")
	}
	events <- fsnotify.Event{Name: filepath.Join(w.rootReal, "app", "b.ts"), Op: fsnotify.Create}
	expectChanges(t, w, change{"app/b.ts", created}, change{"app/c.ts", created}, change{"app/d.ts", created})
}
