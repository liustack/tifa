package devserver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// runLoop runs loop until the test ends and returns the batches it rebuilds.
// hold, when not nil, is called inside each rebuild before it returns.
func runLoop(t *testing.T, c *changes, debounce time.Duration, hold func()) <-chan []change {
	t.Helper()
	return runLoopWith(t, c, debounce, func(context.Context) {}, hold)
}

// runLoopWith is runLoop with catchUp.
func runLoopWith(t *testing.T, c *changes, debounce time.Duration, catchUp func(context.Context), hold func()) <-chan []change {
	t.Helper()
	batches := make(chan []change, 10)
	var running atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop(ctx, c, debounce, catchUp, func(batch []change, _ func()) {
			if running.Add(1) != 1 {
				t.Error("two rebuilds ran at the same time")
			}
			defer running.Add(-1)
			batches <- batch
			if hold != nil {
				hold()
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return batches
}

func receive(t *testing.T, batches <-chan []change) []change {
	t.Helper()
	select {
	case b := <-batches:
		return b
	case <-time.After(5 * time.Second):
		t.Fatal("no rebuild within 5s")
		return nil
	}
}

func TestLoopMergesChangesIntoOneRebuild(t *testing.T) {
	c := newChanges()
	batches := runLoop(t, c, 200*time.Millisecond, nil)
	c.add("app/a.go", "s1", "s2")
	c.add("app/b.tsx", "", "s3")
	c.add("app/a.go", "s2", "s4")
	want := []change{{"app/a.go", modified}, {"app/b.tsx", created}}
	if got := receive(t, batches); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	select {
	case b := <-batches:
		t.Fatalf("unexpected second rebuild %v", b)
	default:
	}
}

func TestLoopSkipsBatchesThatCancelOut(t *testing.T) {
	c := newChanges()
	batches := runLoop(t, c, 10*time.Millisecond, nil)
	c.add("app/a.go", "s1", "s2")
	c.add("app/a.go", "s2", "s1")
	c.add("app/b.go", "s1", "s2") // proves the loop went past the first batch without a rebuild
	if got := receive(t, batches); !slices.Equal(got, []change{{"app/b.go", modified}}) {
		t.Fatalf("got %v, want only app/b.go", got)
	}
}

// expectOutstanding fails when no change up to mark is outstanding.
func expectOutstanding(t *testing.T, c *changes, mark int, what string) {
	t.Helper()
	if c.outstanding(mark) == nil {
		t.Fatalf("%s: a request would no longer wait", what)
	}
}

// expectInRebuild waits until every change up to mark is in a rebuild that began, for up to 5s.
func expectInRebuild(t *testing.T, c *changes, mark int, what string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for ch := c.outstanding(mark); ch != nil; ch = c.outstanding(mark) {
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("%s: a request still waits after 5s", what)
		}
	}
}

// A request waits for the changes recorded before it until the rebuild of their batch began,
// also once the batch is taken and its rebuild has not begun yet. Changes made during that
// rebuild hold later requests until the next one began, and not the earlier request.
func TestLoopEndsTheWaitOnceTheRebuildBegan(t *testing.T) {
	c := newChanges()
	if c.outstanding(c.mark()) != nil {
		t.Fatal("a request waits before any change")
	}
	type rebuild struct {
		batch []change
		began func()
		done  chan struct{}
	}
	rebuilds := make(chan rebuild)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		loop(ctx, c, 10*time.Millisecond, func(context.Context) {}, func(batch []change, began func()) {
			r := rebuild{batch, began, make(chan struct{})}
			select {
			case rebuilds <- r:
			case <-ctx.Done():
				return
			}
			select {
			case <-r.done:
			case <-ctx.Done():
			}
		})
	}()
	defer func() {
		cancel()
		<-stopped
	}()
	next := func() rebuild {
		t.Helper()
		select {
		case r := <-rebuilds:
			return r
		case <-time.After(5 * time.Second):
			t.Fatal("no rebuild within 5s")
			return rebuild{}
		}
	}
	c.add("app/a.tsx", "s1", "s2")
	first := c.mark()
	expectOutstanding(t, c, first, "a change not in a batch")
	r := next()
	expectOutstanding(t, c, first, "the batch is taken and its rebuild has not begun")
	c.add("app/b.tsx", "s1", "s2")
	second := c.mark()
	r.began()
	expectInRebuild(t, c, first, "once the rebuild began")
	expectOutstanding(t, c, second, "a change made during the rebuild")
	close(r.done)
	r = next()
	if !slices.Equal(r.batch, []change{{"app/b.tsx", modified}}) {
		t.Fatalf("second rebuild got %v", r.batch)
	}
	// A rebuild that never calls began ends the wait when it returns.
	close(r.done)
	expectInRebuild(t, c, second, "once the rebuild returned")
}

// A batch that takes only the files that settled, as one file waited as long as a batch waits at
// most, does not end the wait of a request that came after the others changed too.
func TestDueKeepsWaitingForFilesLeftOutOfABatch(t *testing.T) {
	c := newChanges()
	now := time.Now()
	c.add(".env", "s1", "s2")
	c.add("app/a.tsx", "s1", "s2")
	mark := c.mark()
	c.mu.Lock()
	c.first[".env"], c.last[".env"] = now.Add(-2*time.Second), now.Add(-2*time.Second)
	c.mu.Unlock()
	batch, _ := c.due(now, 100*time.Millisecond, time.Second)
	if !slices.Equal(batch, []change{{".env", modified}}) {
		t.Fatalf("the batch is %v, want .env alone", batch)
	}
	c.begin()
	expectOutstanding(t, c, mark, "app/a.tsx is in no rebuild yet")
	batch, _ = c.due(now.Add(time.Second), 100*time.Millisecond, time.Second)
	if !slices.Equal(batch, []change{{"app/a.tsx", modified}}) {
		t.Fatalf("the next batch is %v, want app/a.tsx", batch)
	}
	expectOutstanding(t, c, mark, "the rebuild of app/a.tsx has not begun")
	c.begin()
	expectInRebuild(t, c, mark, "both rebuilds began")
}

// A batch that cancels out, such as a file saved and put back, or a file that only passed through
// as an editor saved, starts no rebuild and ends the wait at once.
func TestLoopEndsTheWaitForABatchThatCancelsOut(t *testing.T) {
	c := newChanges()
	runLoop(t, c, 10*time.Millisecond, nil)
	c.add("app/a.tsx", "s1", "s2")
	c.add("app/a.tsx", "s2", "s1")
	c.add("app/page.tsx.tmp.tsx", "", "s3")
	c.add("app/page.tsx.tmp.tsx", "s3", "")
	expectInRebuild(t, c, c.mark(), "a batch that cancels out")
}

func TestLoopRunsAgainForChangesDuringARebuild(t *testing.T) {
	c := newChanges()
	started := make(chan struct{})
	release := make(chan struct{})
	first := true
	batches := runLoop(t, c, 10*time.Millisecond, func() {
		if first {
			first = false
			close(started)
			<-release
		}
	})
	c.add("app/a.go", "s1", "s2")
	if got := receive(t, batches); !slices.Equal(got, []change{{"app/a.go", modified}}) {
		t.Fatalf("first rebuild got %v", got)
	}
	<-started
	c.add("app/b.tsx", "s1", "s2")
	c.add("app/c.tsx", "s1", "s2")
	close(release)
	want := []change{{"app/b.tsx", modified}, {"app/c.tsx", modified}}
	if got := receive(t, batches); !slices.Equal(got, want) {
		t.Fatalf("second rebuild got %v, want %v", got, want)
	}
}

// A file edited and edited back while a build runs may have been read in between,
// so it is built again even though it ends up as before.
func TestLoopRebuildsFilesRevertedDuringABuild(t *testing.T) {
	c := newChanges()
	started := make(chan struct{})
	release := make(chan struct{})
	first := true
	batches := runLoop(t, c, 10*time.Millisecond, func() {
		if first {
			first = false
			close(started)
			<-release
		}
	})
	c.add("app/a.go", "s1", "s2")
	receive(t, batches)
	<-started
	c.add("app/b.tsx", "A", "B") // the build may read B here
	c.add("app/b.tsx", "B", "A")
	c.add("app/c.tsx", "", "C") // created and removed during the build
	c.add("app/c.tsx", "C", "")
	close(release)
	want := []change{{"app/b.tsx", modified}, {"app/c.tsx", removed}}
	if got := receive(t, batches); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A file that keeps changing, such as a log written every few milliseconds, never lets the files
// go quiet. A batch still starts once it waited maxBatchWaits times the debounce.
func TestLoopRebuildsWhileAFileKeepsChanging(t *testing.T) {
	c := newChanges()
	const debounce = 20 * time.Millisecond
	batches := runLoop(t, c, debounce, nil)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				c.add("app/busy.json", "", time.Now().String())
			}
		}
	}()
	start := time.Now()
	c.add("app/page.tsx", "s1", "s2")
	for {
		select {
		case b := <-batches:
			if slices.Contains(b, change{"app/page.tsx", modified}) {
				if took := time.Since(start); took > 3*maxBatchWaits*debounce {
					t.Fatalf("the edit waited %v for the files to go quiet", took)
				}
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the edit waits as long as another file keeps changing")
		}
	}
}

// A save that lands just as a batch reaches its limit, while another file keeps changing, is
// not cut in two: a file joins a batch once it went quiet for the debounce, or once it kept
// changing for as long as a batch waits at most.
func TestLoopKeepsASaveInOneBatch(t *testing.T) {
	c := newChanges()
	const debounce = 50 * time.Millisecond
	batches := runLoop(t, c, debounce, nil)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				c.add("app/busy.json", "", time.Now().String())
			}
		}
	}()
	time.Sleep(maxBatchWaits*debounce - 30*time.Millisecond)
	c.add("app/route.tsx", "valid", "half saved")
	time.Sleep(debounce * 4 / 5) // past the limit of the batch
	c.add("app/route.tsx", "half saved", "saved")
	var withRoute, withBusy int
	deadline := time.After(maxBatchWaits*debounce + 10*debounce)
	for {
		select {
		case b := <-batches:
			for _, ch := range b {
				switch ch.path {
				case "app/route.tsx":
					withRoute++
				case "app/busy.json":
					withBusy++
				}
			}
			continue
		case <-deadline:
		}
		break
	}
	if withRoute != 1 || withBusy == 0 {
		t.Fatalf("app/route.tsx in %d batches, app/busy.json in %d: want the save in one batch, and the busy file built too", withRoute, withBusy)
	}
}

// The watcher may lag behind the files: the process was stopped for longer than the debounce, such
// as on a busy machine, and the timer of the loop fires while an event of the last write is not
// handled yet. A file saved in place is then empty in the watcher's eyes, and the loop would take
// that as a settled change and rebuild twice. This one is held up when it is about to hash the
// written file.
func TestLoopWaitsForAWatcherThatLagsBehind(t *testing.T) {
	dir := newApp(t)
	rel := "app/store.go"
	var armed atomic.Bool // the hook is set before the watcher runs, and the first scan hashes too
	var calls atomic.Int32
	testHookChecksum = func(name string) {
		if armed.Load() && name == rel && calls.Add(1) == 2 {
			time.Sleep(400 * time.Millisecond)
		}
	}
	t.Cleanup(func() { testHookChecksum = nil })
	w := startWatcher(t, dir)
	armed.Store(true)
	batches := runLoopWith(t, w.changes, 100*time.Millisecond, w.catchUp, nil)
	file := filepath.Join(dir, rel)
	seen := w.changes.mark()
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	for w.changes.mark() == seen { // the watcher hashed the empty file
		time.Sleep(time.Millisecond)
	}
	if err := os.WriteFile(file, []byte("package app // edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, batches); !slices.Equal(got, []change{{rel, modified}}) {
		t.Fatalf("got %v, want one modified %s", got, rel)
	}
	select {
	case got := <-batches:
		t.Fatalf("the save was rebuilt twice, the second time for %v", got)
	case <-time.After(time.Second):
	}
}

// saveInPlace truncates file, waits until the watcher reports it empty, and writes content right
// after, as an editor that saves in place does when a poll looks in between. It returns the
// batches the loop rebuilds within window.
func saveInPlace(t *testing.T, w *watcher, file, content string, window time.Duration) [][]change {
	t.Helper()
	runWatcher(t, w)
	batches := runLoopWith(t, w.changes, 100*time.Millisecond, w.catchUp, nil)
	seen := w.changes.mark()
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	for w.changes.mark() == seen {
		time.Sleep(time.Millisecond)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var got [][]change
	end := time.After(window)
	for {
		select {
		case b := <-batches:
			got = append(got, b)
		case <-end:
			return got
		}
	}
}

// A poll that lands between the truncate and the write of a save in place hashes the empty file,
// and the next poll is far off. The loop looks again before it takes the batch: the save is one
// rebuild, as it is in a directory with a system watch, where every write has an event.
func TestLoopRebuildsOnceForASaveInPlaceSeenByAPoll(t *testing.T) {
	dir := newApp(t)
	w := preparedWatcher(t, dir, func(msg string) { t.Log(msg) })
	w.budget = 0
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	w.minPoll = 1200 * time.Millisecond
	rel := "app/store.go"
	got := saveInPlace(t, w, filepath.Join(dir, rel), "package app // edited", 2500*time.Millisecond)
	if want := [][]change{{{rel, modified}}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got the rebuilds %v, want %v", got, want)
	}
}

// The same for a link to a file outside the app, which only the link poll sees. kqueue has a watch
// on the link's target and reports every write, so this passes there without the poll mattering.
func TestLoopRebuildsOnceForASaveInPlaceSeenByTheLinkPoll(t *testing.T) {
	dir, shared := newApp(t), t.TempDir()
	writeFile(t, shared, "theme.css", "a {}")
	symlink(t, filepath.Join(shared, "theme.css"), filepath.Join(dir, "app/theme.css"))
	w := preparedWatcher(t, dir, func(msg string) { t.Log(msg) })
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	got := saveInPlace(t, w, filepath.Join(shared, "theme.css"), "b {}", 1500*time.Millisecond)
	if want := [][]change{{{"app/theme.css", modified}}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got the rebuilds %v, want %v", got, want)
	}
}
