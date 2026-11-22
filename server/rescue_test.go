package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/ssr"
)

// pageError is an error of the page's code as the SSR bundle reports it: with the number of
// levels of the route tree that could show it (see ssr.Levels).
type pageError struct{ levels int }

func (e pageError) Error() string {
	return fmt.Sprintf("ssr: render page: Error: the page threw (%d levels)", e.levels)
}
func (e pageError) Levels() int { return e.levels }

// placedAt returns the level each render of r was given the error at, -1 for none.
func placedAt(r *renders) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []int
	for _, in := range r.inputs {
		if in.Error == nil {
			out = append(out, -1)
		} else {
			out = append(out, in.Error.Level)
		}
	}
	return out
}

func rescueApp(onError *[]error) App {
	app := frontApp()
	app.Hooks.OnError = func(c *tifa.Ctx, err error) tifa.Error {
		*onError = append(*onError, err)
		return tifa.Error{Status: 502, Message: "try later"}
	}
	return app
}

// A component that throws while the page renders on the server: the page renders again with the
// error OnError made of it at the first level, counted up from the page, that renders, each try
// with that error. OnError sees the error once, the page gets its status, and the answer is never
// cached.
func TestFailedRenderShowsTheErrorAtTheFirstLevelThatRenders(t *testing.T) {
	var reported []error
	s, r := newFrontServer(t, rescueApp(&reported))
	r.result = func(in ssr.Input) (string, error) {
		if in.Error == nil || in.Error.Level == 0 {
			return "", pageError{levels: 3}
		}
		return fmt.Sprintf("<html><body>level %d: %d %s</body></html>", in.Error.Level, in.Error.Status, in.Error.Message), nil
	}
	w := get(s, "/projects/p1")
	if w.Code != 502 || w.Body.String() != "<html><body>level 1: 502 try later</body></html>" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if h := w.Header(); h.Get("Cache-Control") != "no-store" || h.Get("ETag") != "" || h.Get(renderHeader) != "vm" {
		t.Errorf("headers %v", h)
	}
	if len(reported) != 1 || !errors.As(reported[0], new(pageError)) {
		t.Errorf("OnError got %v, want the render's error once", reported)
	}
	if got := placedAt(r); fmt.Sprint(got) != "[-1 0 1]" {
		t.Errorf("renders placed the error at %v", got)
	}
	r.mu.Lock()
	for _, in := range r.inputs {
		if !in.Deadline.IsZero() {
			t.Errorf("a render without ssr.timeout has the deadline %v", in.Deadline)
		}
	}
	r.mu.Unlock()

	// Another page renders for itself, and the same page again within failMemory does not render.
	for i, how := range []string{"vm", "memo"} {
		if w := get(s, "/stats"); w.Code != 502 || w.Header().Get(renderHeader) != how {
			t.Errorf("request %d: %d %v", i, w.Code, w.Header())
		}
	}
	if r.count() != 3+3 {
		t.Errorf("%d renders, want three for each page", r.count())
	}
}

// The page that shows an error has no canonical URL and no versions, but it keeps the site and the
// default language: the render makes share images absolute with the site, and the browser keeps
// it for the navigations that follow.
func TestFailedRenderKeepsTheSiteForTheErrorPage(t *testing.T) {
	var reported []error
	s, r := newFrontServer(t, rescueApp(&reported))
	s.site = "https://a.example.com"
	r.result = func(in ssr.Input) (string, error) {
		if in.Error == nil || in.Error.Level == 0 {
			return "", pageError{levels: 3}
		}
		return "<html><body>error</body></html>", nil
	}
	if w := get(s, "/projects/p1"); w.Code != 502 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.inputs) != 3 {
		t.Fatalf("%d renders", len(r.inputs))
	}
	if first := string(r.inputs[0].Alternates); !strings.Contains(first, `"canonical"`) {
		t.Errorf("the first render has %s, want a canonical", first)
	}
	for _, in := range r.inputs[1:] {
		if got := string(in.Alternates); got != `{"site":"https://a.example.com"}` {
			t.Errorf("the render of the error page has %s, want the site alone", got)
		}
	}
}

// failClock is a clock of the render memo that the test moves.
type failClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *failClock) read() time.Time     { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *failClock) add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

func newFailClock(s *server) *failClock {
	c := &failClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	s.memo.now = c.read
	return c
}

// A render that the page's code fails is remembered for failMemory with the page that shows its
// error: a request for the same input in that time gets the same answer without a render. OnError
// still sees each request, the first with the whole error and the others with its first line. Other
// input renders for itself, and so does the same input once failMemory has passed.
func TestFailedRenderIsRememberedForAFewSeconds(t *testing.T) {
	var reported []error
	s, r := newFrontServer(t, rescueApp(&reported))
	clock := newFailClock(s)
	r.result = func(in ssr.Input) (string, error) {
		if in.Error == nil || in.Error.Level == 0 {
			return "", fmt.Errorf("%w\n    at Page (app/projects/page.tsx:3:9)", pageError{levels: 3})
		}
		return fmt.Sprintf("<html><body>%s level %d: %d</body></html>", in.URL, in.Error.Level, in.Error.Status), nil
	}
	first := get(s, "/projects/p1")
	if first.Code != 502 || first.Header().Get(renderHeader) != "vm" || r.count() != 3 {
		t.Fatalf("%d %v after %d renders", first.Code, first.Header(), r.count())
	}
	clock.add(failMemory - time.Millisecond)
	again := get(s, "/projects/p1")
	if again.Code != 502 || again.Body.String() != first.Body.String() || r.count() != 3 {
		t.Fatalf("%d %s after %d renders", again.Code, again.Body.String(), r.count())
	}
	if h := again.Header(); h.Get("Cache-Control") != "no-store" || h.Get("ETag") != "" || h.Get(renderHeader) != "memo" {
		t.Errorf("headers %v", h)
	}
	if len(reported) != 2 || !errors.As(reported[1], new(pageError)) {
		t.Fatalf("OnError got %v", reported)
	}
	if text := reported[0].Error(); !strings.Contains(text, "at Page") {
		t.Errorf("the first error lacks its stack: %s", text)
	}
	if text := reported[1].Error(); strings.Contains(text, "at Page") || !strings.Contains(text, "the page threw") || !strings.Contains(text, "5s") {
		t.Errorf("the remembered error: %s", text)
	}

	if w := get(s, "/projects/p2"); w.Code != 502 || !strings.Contains(w.Body.String(), "/projects/p2") || r.count() != 6 {
		t.Errorf("other input: %d %s after %d renders", w.Code, w.Body.String(), r.count())
	}
	clock.add(time.Millisecond)
	if w := get(s, "/projects/p1"); w.Code != 502 || w.Header().Get(renderHeader) != "vm" || r.count() != 9 {
		t.Errorf("after failMemory: %d %v after %d renders", w.Code, w.Header(), r.count())
	}
}

// When every level fails, the plain page of Go is remembered too, and the tries are logged once.
func TestFailedRescueIsRemembered(t *testing.T) {
	var reported []error
	s, r := newFrontServer(t, rescueApp(&reported))
	newFailClock(s)
	r.result = func(ssr.Input) (string, error) { return "", pageError{levels: 2} }
	for i := range 3 {
		w := get(s, "/projects/p1")
		if w.Code != 502 || !strings.Contains(w.Body.String(), "<h1>502 Bad Gateway</h1>") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("request %d: %d %v %s", i, w.Code, w.Header(), w.Body.String())
		}
	}
	if r.count() != 3 || len(reported) != 3 {
		t.Errorf("%d renders, OnError saw %d errors", r.count(), len(reported))
	}
}

// tifa dev remembers no failure: the developer fixes the code and reloads.
func TestDevRendersAFailedPageAgain(t *testing.T) {
	var reported []error
	s, r := newFrontServer(t, rescueApp(&reported))
	s.dev = http.NotFoundHandler()
	newFailClock(s)
	r.result = func(in ssr.Input) (string, error) {
		if in.Error == nil {
			return "", pageError{levels: 3}
		}
		return "<html><body>page</body></html>", nil
	}
	for range 2 {
		get(s, "/projects/p1")
	}
	if got := fmt.Sprint(placedAt(r)); got != "[-1 0 -1 0]" {
		t.Errorf("renders placed the error at %s", got)
	}
}

// When every level fails, Go writes its plain page with the status of OnError. The levels end
// where the bundle says, and OnError still sees only the first error.
func TestFailedRenderFallsBackWhenEveryLevelFails(t *testing.T) {
	var reported []error
	s, r := newFrontServer(t, rescueApp(&reported))
	r.result = func(ssr.Input) (string, error) { return "", pageError{levels: 2} }
	w := get(s, "/projects/p1")
	if w.Code != 502 || !strings.Contains(w.Body.String(), "<h1>502 Bad Gateway</h1>") || !strings.Contains(w.Body.String(), "try later") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if got := placedAt(r); fmt.Sprint(got) != "[-1 0 1]" || len(reported) != 1 {
		t.Errorf("renders placed the error at %v, OnError got %v", got, reported)
	}
}

// A render that ran out of time, or whose pool is full, is not tried again, nor is one the bundle
// reports no levels for. A try that runs out of time ends the tries. None of these is remembered,
// as the same input may render the next time: the next request renders again, though a page whose
// code failed stays remembered (again is where both requests placed the error).
func TestFailedRenderIsNotTriedAgainWithoutTime(t *testing.T) {
	timedOut := fmt.Errorf("ssr: render timed out after 3s: %w", context.DeadlineExceeded)
	for name, c := range map[string]struct {
		first, try    error
		placed, again string
	}{
		"timed out":   {timedOut, nil, "[-1]", "[-1 -1]"},
		"busy":        {tifa.ErrSSRBusy, nil, "[-1]", "[-1 -1]"},
		"no levels":   {errors.New("ssr: render left unresolved work after draining microtasks"), nil, "[-1]", "[-1 -1]"},
		"try timeout": {pageError{levels: 3}, timedOut, "[-1 0]", "[-1 0 0]"},
	} {
		t.Run(name, func(t *testing.T) {
			var reported []error
			s, r := newFrontServer(t, rescueApp(&reported))
			newFailClock(s)
			r.result = func(in ssr.Input) (string, error) {
				if in.Error == nil {
					return "", c.first
				}
				return "", c.try
			}
			w := get(s, "/projects/p1")
			if w.Code < 500 || !strings.Contains(w.Body.String(), "<h1>") {
				t.Errorf("%d %s", w.Code, w.Body.String())
			}
			if got := fmt.Sprint(placedAt(r)); got != c.placed {
				t.Errorf("renders placed the error at %s, want %s", got, c.placed)
			}
			get(s, "/projects/p1")
			if got := fmt.Sprint(placedAt(r)); got != c.again {
				t.Errorf("with the second request, renders placed the error at %s, want %s", got, c.again)
			}
		})
	}
}

// The tries share the time budget of the request with the render that failed: each gets the
// deadline ssr.timeout after that render started. A render that ran out of the VM's memory is
// tried again once, on another VM, at the root: the input that did not fit at one level does not
// fit at another.
func TestFailedRenderTriesKeepToTheTimeBudget(t *testing.T) {
	var reported []error
	s, r := newFrontServer(t, rescueApp(&reported))
	s.front.Load().timeout = time.Second
	r.result = func(in ssr.Input) (string, error) {
		return "", fmt.Errorf("%w in startRender", ssr.ErrOutOfMemory)
	}
	before := time.Now()
	get(s, "/projects/p1")
	after := time.Now()
	// The root is at most above the two Layouts, the page and a route the router may add.
	if got := fmt.Sprint(placedAt(r)); got != "[-1 4]" {
		t.Errorf("renders placed the error at %s", got)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, in := range r.inputs[1:] {
		if in.Deadline.Before(before.Add(time.Second)) || in.Deadline.After(after.Add(time.Second)) {
			t.Errorf("deadline %v, want a second after the first render started at %v", in.Deadline, before)
		}
	}
}

// A render out of memory guesses the level of the root. When the bundle then says the page has
// fewer levels, the root is tried there, and nowhere else.
func TestOutOfMemoryTriesTheRootTheBundleNames(t *testing.T) {
	for name, c := range map[string]struct {
		root   error
		placed string
		code   int
	}{
		"renders":       {nil, "[-1 4 1]", 502},
		"out of memory": {ssr.ErrOutOfMemory, "[-1 4 1]", 502},
	} {
		t.Run(name, func(t *testing.T) {
			var reported []error
			s, r := newFrontServer(t, rescueApp(&reported))
			r.result = func(in ssr.Input) (string, error) {
				switch {
				case in.Error == nil:
					return "", ssr.ErrOutOfMemory
				case in.Error.Level > 1:
					return "", pageError{levels: 2}
				case c.root != nil:
					return "", c.root
				}
				return "<html><body>root</body></html>", nil
			}
			w := get(s, "/projects/p1")
			if w.Code != c.code || (c.root == nil) != (w.Body.String() == "<html><body>root</body></html>") {
				t.Errorf("%d %s", w.Code, w.Body.String())
			}
			if got := fmt.Sprint(placedAt(r)); got != c.placed {
				t.Errorf("renders placed the error at %s, want %s", got, c.placed)
			}
		})
	}
}

// Under tifa dev the page shows the Error component and, in the overlay of the dev client, what the
// plain page shows, and it reloads after a rebuild like the plain page.
func TestDevFailedRenderShowsTheErrorOverThePage(t *testing.T) {
	var reported []error
	s, r := newFrontServer(t, rescueApp(&reported))
	s.dev = http.NotFoundHandler()
	r.result = func(in ssr.Input) (string, error) {
		if in.Error == nil {
			return "", fmt.Errorf("%w\n    at Page (app/projects/page.tsx:3:9)", pageError{levels: 3})
		}
		return "<html><body>page</body></html>", nil
	}
	body := get(s, "/projects/p1").Body.String()
	for _, want := range []string{
		"<html><body>page<script>window.__tifaDevErrorPage = true; window.__tifaDevPageError = {",
		`"title":"502 Bad Gateway"`,
		`GET /projects/p1 failed while rendering on the server.\nUsers see: try later\n`,
		`at Page (app/projects/page.tsx:3:9)`,
		`<script src="/__tifa/dev/client.js"></script></body></html>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q: %s", want, body)
		}
	}
}
