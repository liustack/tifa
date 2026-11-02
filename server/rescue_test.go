package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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

	// A page that fails to render is never cached: it renders again on every request.
	for range 2 {
		if w := get(s, "/stats"); w.Code != 502 || w.Header().Get(renderHeader) != "vm" {
			t.Errorf("cached page: %d %v", w.Code, w.Header())
		}
	}
	if r.count() != 3+2*3 {
		t.Errorf("%d renders, want each request to render three times", r.count())
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
// reports no levels for. A try that runs out of time ends the tries.
func TestFailedRenderIsNotTriedAgainWithoutTime(t *testing.T) {
	timedOut := fmt.Errorf("ssr: render timed out after 3s: %w", context.DeadlineExceeded)
	for name, c := range map[string]struct {
		first, try error
		placed     string
	}{
		"timed out":   {timedOut, nil, "[-1]"},
		"busy":        {tifa.ErrSSRBusy, nil, "[-1]"},
		"no levels":   {errors.New("ssr: render left unresolved work after draining microtasks"), nil, "[-1]"},
		"try timeout": {pageError{levels: 3}, timedOut, "[-1 0]"},
	} {
		t.Run(name, func(t *testing.T) {
			var reported []error
			s, r := newFrontServer(t, rescueApp(&reported))
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
		})
	}
}

// The tries share the time budget of the request with the render that failed: each gets the
// deadline ssr.timeout after that render started. A render that ran out of the VM's memory is
// tried again, on another VM, as many levels as the page has.
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
	// The root, the two Layouts and the page, and one more for a route the router may add.
	if got := fmt.Sprint(placedAt(r)); got != "[-1 0 1 2 3 4]" {
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
