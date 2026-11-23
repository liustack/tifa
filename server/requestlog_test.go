package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// logRecords keeps what a slog handler gets, so a test can read the log lines.
type logRecords struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *logRecords) Enabled(context.Context, slog.Level) bool { return true }
func (l *logRecords) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *logRecords) WithGroup(string) slog.Handler            { return l }
func (l *logRecords) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	l.records = append(l.records, r)
	l.mu.Unlock()
	return nil
}

// requests returns the attributes of each "request" line, as maps.
func (l *logRecords) requests() []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	var lines []map[string]any
	for _, r := range l.records {
		if r.Message != "request" {
			continue
		}
		line := map[string]any{}
		r.Attrs(func(a slog.Attr) bool {
			line[a.Key] = a.Value.Any()
			return true
		})
		lines = append(lines, line)
	}
	return lines
}

// The code of the request log below is the recipe of docs/guides/logs.md, kept the same by hand:
// change one, change the other.

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush, SetWriteDeadline and Hijack below.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(app http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			app.ServeHTTP(w, r) // bundle files: no line
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		app.ServeHTTP(sw, r)
		slog.InfoContext(r.Context(), "request",
			"method", r.Method, "route", r.Pattern, "status", sw.status, "duration", time.Since(start))
	})
}

func TestRequestLogRecipe(t *testing.T) {
	logs := &logRecords{}
	before := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(before) })
	t.Cleanup(func() { tifa.SetHTTPHandler(nil) })

	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	app := frontApp()
	app.Routes = append(app.Routes,
		Route{ID: "app/boom", Path: "/boom", GET: func(c *tifa.Ctx) error { return errors.New("boom") }},
		Route{ID: "app/stream", Path: "/stream", GET: func(c *tifa.Ctx) error {
			c.SetHeader("Content-Type", "text/event-stream")
			io.WriteString(c.Writer(), "data: first\n\n")
			if err := http.NewResponseController(c.Writer()).Flush(); err != nil {
				return err
			}
			<-release
			return nil
		}},
	)
	app.Hooks.OnStart = func(context.Context) error {
		tifa.SetHTTPHandler(logRequests)
		return nil
	}
	base := serveApp(t, app, false, "--dist", bundleWithFiles(t))

	// The line comes after the handler returns, so wait for the request to be logged.
	logged := func(n int) []map[string]any {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if lines := logs.requests(); len(lines) >= n {
				return lines
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d request lines, want %d", len(logs.requests()), n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	last := func(n int) map[string]any { return logged(n)[n-1] }
	check := func(line map[string]any, method, route string, status int) {
		t.Helper()
		if line["method"] != method || line["route"] != route || line["status"] != int64(status) {
			t.Errorf("logged %v, want %s %q %d", line, method, route, status)
		}
		if d, ok := line["duration"].(time.Duration); !ok || d <= 0 {
			t.Errorf("duration %v", line["duration"])
		}
	}

	fetch(t, "GET", base+"/projects/p1")
	check(last(1), "GET", "/projects/{projectId}", 200)

	if code, _ := fetch(t, "GET", base+"/nowhere"); code != 404 {
		t.Fatalf("unmatched URL %d", code)
	}
	check(last(2), "GET", "", 404)

	if code, _ := fetch(t, "GET", base+"/boom"); code != 500 {
		t.Fatalf("handler error %d", code)
	}
	check(last(3), "GET", "/boom", 500)

	fetch(t, "GET", base+"/assets/app.js")
	fetch(t, "GET", base+"/robots.txt")
	check(last(4), "GET", "", 200) // a public file has no route, and the /assets/ file above has no line
	if n := len(logs.requests()); n != 4 {
		t.Errorf("%d lines, want 4: %v", n, logs.requests())
	}

	// A stream flushes through the wrapper while its handler is still running.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(base + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("first event %q, %v", line, err)
	}
	if n := len(logs.requests()); n != 4 {
		t.Errorf("the stream was logged before it ended: %d lines", n)
	}
	once.Do(func() { close(release) })
	check(last(5), "GET", "/stream", 200)
}
