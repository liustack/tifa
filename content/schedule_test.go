package content

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/content"
)

// fakeClock is a clock that moves only when the test says so. Its timers fire, in the order of
// their times, in the goroutine of advance.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	f       func()
	stopped bool
}

// useClock makes a fake clock at start the clock of the collections until t ends.
func useClock(t *testing.T, start time.Time) *fakeClock {
	t.Helper()
	c := &fakeClock{now: start}
	t.Cleanup(content.SetClock(content.Clock{Now: c.Now, AfterFunc: c.AfterFunc}))
	return c
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	tm := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, tm)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !tm.stopped
		tm.stopped = true
		return was
	}
}

// pending returns the times of the timers that have neither fired nor been stopped.
func (c *fakeClock) pending() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	var at []time.Time
	for _, tm := range c.timers {
		if !tm.stopped {
			at = append(at, tm.at)
		}
	}
	return at
}

// advance moves the clock by d and fires the timers whose time has come.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var due *fakeTimer
		for _, tm := range c.timers {
			if !tm.stopped && !tm.at.After(c.now) && (due == nil || tm.at.Before(due.at)) {
				due = tm
			}
		}
		if due != nil {
			due.stopped = true
		}
		c.mu.Unlock()
		if due == nil {
			return
		}
		due.f()
	}
}

// captureLog sends the log of the program to a buffer until t ends.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// watch watches the collections until t ends and counts the calls of published.
func watch(t *testing.T) func() int {
	t.Helper()
	var mu sync.Mutex
	n := 0
	stop := content.Watch(func() { mu.Lock(); n++; mu.Unlock() })
	t.Cleanup(stop)
	return func() int { mu.Lock(); defer mu.Unlock(); return n }
}

func ids(posts []post) []string {
	var out []string
	for _, p := range posts {
		out = append(out, p.Lang+"/"+p.ID)
	}
	return out
}

func langs(ts []Translation) []string {
	var out []string
	for _, x := range ts {
		out = append(out, x.Lang)
	}
	return out
}

var start = time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

func TestEntriesArePublishedAtTheirDate(t *testing.T) {
	clock := useClock(t, start)
	log := captureLog(t)
	at9 := time.Date(2026, 11, 20, 9, 0, 0, 0, time.FixedZone("", 8*3600)) // 01:00 UTC
	if err := load(t, content.File{"testblog": {
		record("202611/launch", "zh-CN", "launch", "launch", start.Add(-time.Hour), "新版本"),
		record("202611/release-notes", "en", "release-notes", "launch", at9, "Release"),
		record("202611/more", "en", "more", "202611/more", at9, "More"),
		record("202611/later", "en", "later", "202611/later", start.Add(2*time.Hour), "Later"),
	}}); err != nil {
		t.Fatal(err)
	}
	published := watch(t)
	if got := ids(blog.All()); !slices.Equal(got, []string{"zh-CN/202611/launch"}) {
		t.Fatalf("All before the date = %q", got)
	}
	launch, _ := blog.Get("zh-CN", "202611/launch")
	if got := langs(launch.Translations); !slices.Equal(got, []string{"zh-CN"}) {
		t.Errorf("Translations before the date = %q", got)
	}
	if _, err := blog.Get("en", "202611/release-notes"); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("Get before the date: %v", err)
	}
	if got := clock.pending(); len(got) != 1 || !got[0].Equal(at9) {
		t.Fatalf("timers %v, want one at %v", got, at9)
	}

	clock.advance(time.Hour - time.Nanosecond)
	if len(blog.All()) != 1 || published() != 0 {
		t.Fatalf("a nanosecond before the date: %q, %d calls", ids(blog.All()), published())
	}
	clock.advance(time.Nanosecond)
	// Both entries of 09:00 come at once, with one call and one line of log.
	if got, want := ids(blog.All()), []string{"zh-CN/202611/launch", "en/202611/more", "en/202611/release-notes"}; !slices.Equal(got, want) {
		t.Errorf("All at the date = %q, want %q", got, want)
	}
	launch, _ = blog.Get("zh-CN", "202611/launch")
	notes, err := blog.Get("en", "202611/release-notes")
	if err != nil || !slices.Equal(langs(launch.Translations), []string{"zh-CN", "en"}) || !slices.Equal(langs(notes.Translations), []string{"zh-CN", "en"}) {
		t.Errorf("Translations at the date = %q and %q, %v", langs(launch.Translations), langs(notes.Translations), err)
	}
	if published() != 1 {
		t.Errorf("published was called %d times", published())
	}
	if want := "level=INFO msg=\"tifa: content: testblog en/202611/more and en/202611/release-notes are published (date 2026-11-20 09:00 +08:00)\"\n"; log.String() != want {
		t.Errorf("log %q\nwant %q", log.String(), want)
	}
	// The timer is set for the next date.
	if got := clock.pending(); len(got) != 1 || !got[0].Equal(start.Add(2*time.Hour)) {
		t.Fatalf("timers %v, want one at 02:00", got)
	}
	log.Reset()
	clock.advance(time.Hour)
	if len(blog.All()) != 4 || published() != 2 || len(clock.pending()) != 0 {
		t.Errorf("at the last date: %q, %d calls, timers %v", ids(blog.All()), published(), clock.pending())
	}
	if want := "level=INFO msg=\"tifa: content: testblog en/202611/later is published (date 2026-11-20 02:00 +00:00)\"\n"; log.String() != want {
		t.Errorf("log %q\nwant %q", log.String(), want)
	}
}

// A timer that fires late, as after the machine slept, publishes every entry whose date has come.
func TestLateTimerPublishesEverythingDue(t *testing.T) {
	clock := useClock(t, start)
	captureLog(t)
	if err := load(t, content.File{"testblog": {
		record("a", "en", "a", "a", start.Add(time.Minute), "A"),
		record("b", "en", "b", "b", start.Add(2*time.Minute), "B"),
	}}); err != nil {
		t.Fatal(err)
	}
	published := watch(t)
	clock.mu.Lock()
	clock.now = clock.now.Add(time.Hour)
	clock.mu.Unlock()
	clock.advance(0)
	if len(blog.All()) != 2 || published() != 1 || len(clock.pending()) != 0 {
		t.Errorf("%q, %d calls, timers %v", ids(blog.All()), published(), clock.pending())
	}
}

func TestStoppedWatchStopsTheTimer(t *testing.T) {
	clock := useClock(t, start)
	n := 0
	stop := content.Watch(func() { n++ })
	if err := load(t, content.File{"testblog": {record("a", "en", "a", "a", start.Add(time.Minute), "A")}}); err != nil {
		t.Fatal(err)
	}
	if len(clock.pending()) != 1 {
		t.Fatalf("timers %v", clock.pending())
	}
	n = 0
	stop()
	if len(clock.pending()) != 0 {
		t.Errorf("a timer is left: %v", clock.pending())
	}
	clock.advance(time.Hour)
	if n != 0 {
		t.Errorf("published was called %d times after stop", n)
	}
	// Without a watcher no timer is set, and the entries are judged when they are loaded.
	if err := content.LoadFile(content.File{"testblog": {record("b", "en", "b", "b", start.Add(2*time.Hour), "B")}}); err != nil {
		t.Fatal(err)
	}
	if len(clock.pending()) != 0 {
		t.Errorf("a timer without a watcher: %v", clock.pending())
	}
}

// Install gives the collections new entries, as tifa dev does when content changes, and sets the
// timer for the new dates.
func TestInstallSetsTheTimerAgain(t *testing.T) {
	clock := useClock(t, start)
	captureLog(t)
	published := watch(t)
	if err := load(t, content.File{"testblog": {record("a", "en", "a", "a", start.Add(time.Hour), "A")}}); err != nil {
		t.Fatal(err)
	}
	calls := published()
	if err := content.Install(content.File{"testblog": {record("b", "en", "b", "b", start.Add(time.Minute), "B")}}, nil); err != nil {
		t.Fatal(err)
	}
	if published() != calls+1 {
		t.Errorf("Install did not tell the watchers: %d calls, then %d", calls, published())
	}
	if got := clock.pending(); len(got) != 1 || !got[0].Equal(start.Add(time.Minute)) {
		t.Fatalf("timers %v, want one at 00:01", got)
	}
	clock.advance(time.Minute)
	if got := ids(blog.All()); !slices.Equal(got, []string{"en/b"}) {
		t.Errorf("All = %q", got)
	}
	// A failed Install changes nothing.
	if err := content.Install(content.File{"testblog": {record("c", "en", "c", "c", start.Add(time.Hour), "bad")}}, nil); err == nil {
		t.Fatal("Install of an entry that fails Validate")
	}
	if got := ids(blog.All()); !slices.Equal(got, []string{"en/b"}) || len(clock.pending()) != 0 {
		t.Errorf("after a failed Install: %q, timers %v", got, clock.pending())
	}
}

// SetClock judges the entries at the time of the new clock at once, and a clock that does not move
// sets no timer.
func TestSetClockJudgesTheEntriesAgain(t *testing.T) {
	useClock(t, start)
	watch(t)
	if err := load(t, content.File{"testblog": {record("a", "en", "a", "a", start.Add(time.Hour), "A")}}); err != nil {
		t.Fatal(err)
	}
	restore := content.SetClock(content.Clock{Now: func() time.Time { return start.Add(2 * time.Hour) }})
	if got := ids(blog.All()); !slices.Equal(got, []string{"en/a"}) {
		t.Errorf("All after the date = %q", got)
	}
	restore()
	if got := ids(blog.All()); len(got) != 0 {
		t.Errorf("All back before the date = %q", got)
	}
}

// The timer runs on the real clock too: an entry a few milliseconds away shows up by itself.
func TestEntryShowsUpOnTheRealClock(t *testing.T) {
	captureLog(t)
	done := make(chan struct{}, 1)
	stop := content.Watch(func() { done <- struct{}{} })
	defer stop()
	if err := load(t, content.File{"testblog": {record("a", "en", "a", "a", time.Now().Add(20*time.Millisecond), "A")}}); err != nil {
		t.Fatal(err)
	}
	// Loading tells the watcher once.
	<-done
	if len(blog.All()) != 0 {
		t.Fatal("the entry is out before its date")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the entry was not published")
	}
	if len(blog.All()) != 1 {
		t.Errorf("All = %q", ids(blog.All()))
	}
}
