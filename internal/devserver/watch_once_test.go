package devserver

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The watcher lists a directory again whenever its entries change, as they do at every atomic
// save in it, and decides again about each entry. The same decision gives the same warning,
// which the watcher gives once, and keeps what it watches and polls.

// count returns how many warnings mention substr.
func (w *warnings) count(substr string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, m := range w.msgs {
		if strings.Contains(m, substr) {
			n++
		}
	}
	return n
}

// saveTimes saves rel atomically n times and waits for each save to be reported.
func saveTimes(t *testing.T, dir string, w *watcher, rel string, n int) {
	t.Helper()
	for i := range n {
		saveFile(t, dir, rel, fmt.Sprintf("package app // %d", i))
		expectBatch(t, dir, w, change{rel, modified})
	}
}

// watchState is what the watcher watches and polls, to compare before and after.
type watchState struct {
	watches, polled []string
	dirs            int
}

func stateOf(w *watcher) watchState {
	return watchState{
		watches: slices.Sorted(maps.Keys(w.watches)),
		polled:  slices.Sorted(maps.Keys(w.polled)),
		dirs:    len(w.owner),
	}
}

func (s watchState) equal(o watchState) bool {
	return slices.Equal(s.watches, o.watches) && slices.Equal(s.polled, o.polled) && s.dirs == o.dirs
}

// runUntil runs w until stop is called or the test ends. stop returns the state the run left.
func runUntil(t *testing.T, w *watcher) (stop func() watchState) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.run(ctx) }()
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			cancel()
			<-done
		}
	})
	return func() watchState {
		stopped = true
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		return stateOf(w)
	}
}

// A link to a directory that is already watched is warned about once, and stays unwatched.
func TestWatcherWarnsOnceAboutALinkedDirectory(t *testing.T) {
	dir, shared := newApp(t), t.TempDir()
	writeFile(t, shared, "button.tsx", "export {}")
	symlink(t, shared, filepath.Join(dir, "app/a"))
	symlink(t, shared, filepath.Join(dir, "app/b"))
	var warned warnings
	w := preparedWatcher(t, dir, warned.add)
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	before := stateOf(w)
	stop := runUntil(t, w)
	saveTimes(t, dir, w, "app/store.go", 3)
	writeFile(t, shared, "button.tsx", "export const edited = 1")
	expectBatch(t, dir, w, change{"app/a/button.tsx", modified})
	if after := stop(); !after.equal(before) {
		t.Fatalf("the saves changed what is watched: %+v, before %+v", after, before)
	}
	if n := warned.count("not watching app/b"); n != 1 {
		t.Fatalf("warned %d times about app/b, want once: %q", n, warned.msgs)
	}
}

// A link that loops is warned about once.
func TestWatcherWarnsOnceAboutALinkLoop(t *testing.T) {
	dir := newApp(t)
	symlink(t, "self", filepath.Join(dir, "app/self"))
	var warned warnings
	w := preparedWatcher(t, dir, warned.add)
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	before := stateOf(w)
	stop := runUntil(t, w)
	saveTimes(t, dir, w, "app/store.go", 3)
	if after := stop(); !after.equal(before) {
		t.Fatalf("the saves changed what is watched: %+v, before %+v", after, before)
	}
	if n := warned.count("skipping app/self"); n != 1 {
		t.Fatalf("warned %d times about app/self, want once: %q", n, warned.msgs)
	}
}

// The directories found once the budget ran out at the start stay polled, and the watcher says
// so once.
func TestWatcherKeepsTheDirectoriesBeyondTheBudgetPolled(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/store.go", "package app")
	writeFile(t, dir, "marker.ts", "start")
	writeFile(t, dir, "z/x.ts", "export {}")
	var warned warnings
	w := preparedWatcher(t, dir, warned.add)
	w.budget = 2 // the root and app, z is beyond
	if watchesFiles {
		w.budget = 4 // and app/store.go and marker.ts
	}
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	if !w.polled["z"] || len(w.polled) != 1 {
		t.Fatalf("polled %v, want z", w.polled)
	}
	before := stateOf(w)
	stop := runUntil(t, w)
	saveTimes(t, dir, w, "app/store.go", 3)
	if after := stop(); !after.equal(before) {
		t.Fatalf("the saves changed what is watched: %+v, before %+v", after, before)
	}
	if n := warned.count("would use up"); n != 1 {
		t.Fatalf("warned %d times about the budget, want once: %q", n, warned.msgs)
	}
}

// A directory that grew past the budget stays polled, and the watcher says so once.
func TestWatcherKeepsAGrownDirectoryPolled(t *testing.T) {
	if !watchesFiles {
		t.Skip("a new file takes no watch of its own on this system")
	}
	dir := newApp(t)
	var warned warnings
	w := preparedWatcher(t, dir, warned.add)
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	w.budget = w.watchCount() + 2
	stop := runUntil(t, w)
	var want []change
	for i := range 5 {
		name := fmt.Sprintf("app/f%d.ts", i)
		writeFile(t, dir, name, "export {}")
		want = append(want, change{name, created})
	}
	expectChanges(t, w, want...)
	if !w.polled["app"] {
		t.Fatalf("app is not polled: %v", w.polled)
	}
	saveTimes(t, dir, w, "app/store.go", 3)
	after := stop()
	if !w.polled["app"] {
		t.Fatalf("app is no longer polled after the saves: %+v", after)
	}
	if n := warned.count("app grew past"); n != 1 {
		t.Fatalf("warned %d times about app, want once: %q", n, warned.msgs)
	}
}
