package content

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/liustack/tifa/internal/core"
)

// An entry whose date is still to come is hidden until that moment. Each collection keeps the
// entries that can be seen now (see Collection.Show), and one timer for the first date still to
// come, a minute at most (maxWait), shows the entries whose date has come by the wall clock,
// tells the watchers and sets itself for the next date.

// Clock judges which entries can be seen, and wakes the program when the next one can.
type Clock struct {
	Now func() time.Time
	// AfterFunc calls f in a goroutine of its own after d, as time.AfterFunc, and returns the
	// function that stops it. It is nil for a clock that does not move, which sets no timer.
	AfterFunc func(d time.Duration, f func()) (stop func() bool)
}

// Shown is an entry that can be seen from now on.
type Shown struct {
	Collection, Lang, ID string
	Date                 time.Time
}

var realClock = Clock{Now: time.Now, AfterFunc: func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }}

var clock atomic.Pointer[Clock]

func init() { clock.Store(&realClock) }

// Now is the time of the clock of the collections.
func Now() time.Time { return clock.Load().Now() }

// futureShown is set under tifa dev, which shows the entries whose date is still to come.
var futureShown atomic.Bool

// ShowFuture(true) makes every entry visible from the next Install on, also one whose date is
// still to come, as tifa dev shows them to the writer: no date is in the future then, so no timer
// is set. server.DevMain calls it before the content is loaded. A built program never calls it.
func ShowFuture(on bool) { futureShown.Store(on) }

// endOfTime comes after every date of an entry.
var endOfTime = time.Unix(1<<62, 0)

// VisibleAt is the time the collections judge their entries by: Now, or under ShowFuture(true) a
// time after every date.
func VisibleAt() time.Time {
	if futureShown.Load() {
		return endOfTime
	}
	return Now()
}

// The state of the timer, under timerMu. Loading, the timer and a new clock take turns under it.
var (
	timerMu  sync.Mutex
	watchers = map[*func()]bool{}
	// stopTimer stops the timer that is set, nil without one. generation tells a timer that fires
	// after it was stopped or replaced that it is not the one any more.
	stopTimer  func() bool
	generation uint64
)

// Watch has the collections show each entry at its date: while a watcher is there, a timer is
// set for the first date still to come. changed is called after entries were shown, and after
// Install gave the collections new entries. stop removes the watcher, and the timer with the last
// one. Once stop returns, changed is not called again. changed runs while no collection can
// change, so it must not call Watch, Install or SetClock.
func Watch(changed func()) (stop func()) {
	timerMu.Lock()
	defer timerMu.Unlock()
	key := &changed
	watchers[key] = true
	// An entry whose date came since the collections were loaded is published now.
	refreshLocked(true)
	return func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		delete(watchers, key)
		if len(watchers) == 0 {
			setTimerLocked(time.Time{})
		}
	}
}

// Install makes the records of f the data of every declared collection, judged at the time of the
// clock, sets the timer for the first date still to come and tells the watchers. It runs Validate
// on every entry first, and changes nothing when one fails. dist holds the bodies of the records,
// nil without the files of a build.
func Install(f File, dist fs.FS) error {
	var commits []func()
	for _, c := range Collections() {
		commit, err := c.Decode(f[c.Source.Dir], dist)
		if err != nil {
			return fmt.Errorf("tifa: content: %w", err)
		}
		commits = append(commits, commit)
	}
	timerMu.Lock()
	defer timerMu.Unlock()
	for _, commit := range commits {
		commit()
	}
	refreshLocked(false)
	notifyLocked()
	return nil
}

// SetClock makes c the clock of the collections, judges every entry again by it and sets the
// timer by it. restore puts the clock that was there back, and judges the entries by it.
func SetClock(c Clock) (restore func()) {
	set := func(c *Clock) {
		timerMu.Lock()
		defer timerMu.Unlock()
		clock.Store(c)
		refreshLocked(false)
		notifyLocked()
	}
	old := clock.Load()
	set(&c)
	return func() { set(old) }
}

// fire is the timer of generation gen: it shows the entries whose date has come.
func fire(gen uint64) {
	timerMu.Lock()
	defer timerMu.Unlock()
	if gen != generation {
		return
	}
	stopTimer = nil
	refreshLocked(true)
}

// refreshLocked shows every entry whose date has come and sets the timer for the next date. When
// entries were shown and published is set, it logs them and tells the watchers.
func refreshLocked(published bool) {
	now := VisibleAt()
	var shown []Shown
	var next time.Time
	for _, c := range Collections() {
		if c.Show == nil {
			continue
		}
		s, n := c.Show(now)
		shown = append(shown, s...)
		if !n.IsZero() && (next.IsZero() || n.Before(next)) {
			next = n
		}
	}
	setTimerLocked(next)
	if published && len(shown) > 0 {
		core.Logger().Info("tifa: content: " + publishedLine(shown))
		notifyLocked()
	}
}

// maxWait is the longest a timer waits before it judges the entries again. A timer waits on the
// monotonic clock, and the dates are on the wall clock: when the clock of the machine is set, or
// the monotonic clock stands still while the machine sleeps, an entry comes out at most this late.
const maxWait = time.Minute

// setTimerLocked sets the timer for at, or for maxWait when at is further, or stops it for the zero
// time. Without watchers, or with a clock that does not move, there is no timer.
func setTimerLocked(at time.Time) {
	if stopTimer != nil {
		stopTimer()
		stopTimer = nil
	}
	generation++
	c := clock.Load()
	if at.IsZero() || len(watchers) == 0 || c.AfterFunc == nil {
		return
	}
	gen := generation
	stopTimer = c.AfterFunc(min(at.Sub(c.Now()), maxWait), func() { fire(gen) })
}

func notifyLocked() {
	for w := range watchers {
		(*w)()
	}
}

// publishedLine says which entries are published, such as
// "blog zh-CN/202611/launch and en/202611/release-notes are published (date 2026-11-20 09:00 +08:00)".
func publishedLine(shown []Shown) string {
	var names, dates []string
	for i, s := range shown {
		name := path.Join(s.Lang, s.ID)
		if i == 0 || shown[i-1].Collection != s.Collection {
			name = s.Collection + " " + name
		}
		names = append(names, name)
		if d := FormatDate(s.Date); !slices.Contains(dates, d) {
			dates = append(dates, d)
		}
	}
	verb, noun := "are", "date"
	if len(shown) == 1 {
		verb = "is"
	}
	if len(dates) > 1 {
		noun = "dates"
	}
	return joinAnd(names) + " " + verb + " published (" + noun + " " + strings.Join(dates, ", ") + ")"
}

// FormatDate writes the date of an entry with its zone, to the minute unless it has seconds:
// 2026-11-20 09:00 +08:00.
func FormatDate(t time.Time) string {
	if t.Second() != 0 || t.Nanosecond() != 0 {
		return t.Format("2006-01-02 15:04:05 -07:00")
	}
	return t.Format("2006-01-02 15:04 -07:00")
}

// joinAnd joins a list as English does: a, b and c.
func joinAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
