package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/liustack/tifa/content"
	internalcontent "github.com/liustack/tifa/internal/content"
)

type serverPost struct {
	content.Entry
	Title string `json:"title"`
}

func (p serverPost) Validate() error {
	if p.Title == "" {
		return errors.New("title is empty")
	}
	return nil
}

// definePosts declares a collection for the test t only: the other tests of the server run apps
// without content.json, which a declared collection needs.
func definePosts(t *testing.T) *content.Collection[serverPost] {
	c := content.Define[serverPost](content.Source{Dir: "serverposts"})
	t.Cleanup(func() { internalcontent.Unregister(c) })
	return c
}

// distWithContent is the dist/ of a build without pages whose content.json holds one entry with
// the title title.
func distWithContent(title string) fstest.MapFS {
	return fstest.MapFS{
		"dist/build.json":   {Data: []byte(`{"buildId":"t","head":""}`)},
		"dist/content.json": {Data: []byte(`{"serverposts":[{"id":"a","slug":"a","path":"a","group":"a","words":2,"source":"content/serverposts/a.md","data":{"title":"` + title + `"}}]}`)},
	}
}

var errStop = errors.New("stop here")

func TestContentIsLoadedBeforeOnStart(t *testing.T) {
	serverPosts := definePosts(t)
	var titles []string
	app := App{Dist: distWithContent("A"), Hooks: Hooks{OnStart: func(context.Context) error {
		for _, p := range serverPosts.All() {
			titles = append(titles, p.Title)
		}
		return errStop
	}}}
	_, _, err := runApp(t, context.Background(), app, nil, "--server-addr", "127.0.0.1:0")
	if !errors.Is(err, errStop) {
		t.Fatalf("serve: %v", err)
	}
	if !slices.Equal(titles, []string{"A"}) {
		t.Errorf("OnStart saw %q", titles)
	}
}

func TestContentThatFailsValidateStopsTheStart(t *testing.T) {
	definePosts(t)
	started := false
	app := App{Dist: distWithContent(""), Hooks: Hooks{OnStart: func(context.Context) error {
		started = true
		return errStop
	}}}
	_, _, err := runApp(t, context.Background(), app, nil, "--server-addr", "127.0.0.1:0")
	if err == nil || err.Error() != "tifa: content: content/serverposts/a.md: Validate: title is empty" {
		t.Fatalf("serve: %v", err)
	}
	if started {
		t.Error("OnStart ran")
	}
}

func TestContentNeedsTheContentFileOfTheBuild(t *testing.T) {
	definePosts(t)
	dist := fstest.MapFS{"dist/build.json": {Data: []byte(`{"buildId":"t","head":""}`)}}
	_, _, err := runApp(t, context.Background(), App{Dist: dist}, nil, "--server-addr", "127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "content.json") {
		t.Fatalf("serve: %v", err)
	}
}

// stepClock is a clock of the content collections that moves when the test says so. Its timers
// fire in the goroutine of advance.
type stepClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*stepTimer
}

type stepTimer struct {
	at   time.Time
	f    func()
	done bool
}

func (c *stepClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *stepClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	tm := &stepTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, tm)
	return func() bool { c.mu.Lock(); defer c.mu.Unlock(); was := !tm.done; tm.done = true; return was }
}

// pending counts the timers that neither fired nor were stopped.
func (c *stepClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, tm := range c.timers {
		if !tm.done {
			n++
		}
	}
	return n
}

func (c *stepClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*stepTimer
	for _, tm := range c.timers {
		if !tm.done && !tm.at.After(c.now) {
			tm.done = true
			due = append(due, tm)
		}
	}
	c.mu.Unlock()
	for _, tm := range due {
		tm.f()
	}
}

// The program publishes an entry at its date while it runs, logs it, and stops the timer when it
// shuts down.
func TestContentIsPublishedWhileTheProgramRuns(t *testing.T) {
	serverPosts := definePosts(t)
	clock := &stepClock{now: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)}
	t.Cleanup(internalcontent.SetClock(internalcontent.Clock{Now: clock.Now, AfterFunc: clock.AfterFunc}))
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	dist := fstest.MapFS{
		"dist/build.json":   {Data: []byte(`{"buildId":"t","head":""}`)},
		"dist/content.json": {Data: []byte(`{"serverposts":[{"id":"a","slug":"a","path":"a","group":"a","date":"2026-11-20T00:01:00Z","words":2,"source":"content/serverposts/a.md","data":{"title":"A"}}]}`)},
	}
	var before, after, timers int
	app := App{Dist: dist, Hooks: Hooks{OnStart: func(context.Context) error {
		before, timers = len(serverPosts.All()), clock.pending()
		clock.advance(time.Minute)
		after = len(serverPosts.All())
		return errStop
	}}}
	_, _, err := runApp(t, context.Background(), app, nil, "--server-addr", "127.0.0.1:0")
	if !errors.Is(err, errStop) {
		t.Fatalf("serve: %v", err)
	}
	if before != 0 || timers != 1 || after != 1 {
		t.Errorf("before the date %d entries and %d timers, at the date %d entries", before, timers, after)
	}
	if !strings.Contains(log.String(), `msg="tifa: content: serverposts a is published (date 2026-11-20 00:01 +00:00)"`) {
		t.Errorf("log:\n%s", log.String())
	}
	// The program has stopped, and so has the timer: new entries set none.
	err = internalcontent.LoadFile(internalcontent.File{"serverposts": {{ID: "b", Slug: "b", Path: "b", Group: "b", Date: clock.Now().Add(time.Hour), Source: "b.md", Data: []byte(`{"title":"B"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if clock.pending() != 0 {
		t.Errorf("%d timers after the program stopped", clock.pending())
	}
}
