package server

import (
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/liustack/tifa"
)

// requestLines returns what logs holds of the request lines, with the time and the duration
// replaced, so that a test can compare them.
func requestLines(logs string) []string {
	var lines []string
	duration := regexp.MustCompile(` \d+(\.\d+)?(ns|µs|ms|s) `)
	level := regexp.MustCompile(`(?:^|\s)INFO (.*)$`)
	for _, line := range strings.Split(logs, "\n") {
		m := level.FindStringSubmatch(line) // the time in front is there or not, as other tests left log.SetFlags
		if m == nil || !regexp.MustCompile(`^(GET|POST|DELETE) `).MatchString(m[1]) {
			continue
		}
		lines = append(lines, duration.ReplaceAllString(m[1], " <t> "))
	}
	return lines
}

// Under DevMain the app writes a line for each request of a route: the method, the URL, the status,
// the time, the route and what the render memo did. A navigation data request says data. Files of
// the bundle and of the public folder, and the requests of tifa dev, get no line, and a built app
// writes none at all.
func TestDevLogsEachRequest(t *testing.T) {
	defer log.SetOutput(os.Stderr)
	app := frontApp()
	app.Routes = append(app.Routes, Route{ID: "app/api/echo", Path: "/api/echo", SSR: true,
		POST: func(c *tifa.Ctx) error { return c.NoContent(http.StatusNoContent) }})
	run := func(dev bool) []string {
		var logs syncBuffer
		log.SetOutput(&logs)
		dir := t.TempDir()
		writeBundle(t, dir, "same")
		base := serveApp(t, app, dev, "--dist", dir)
		for _, req := range [][2]string{
			{"GET", "/stats?tab=2"}, {"GET", "/stats?tab=2"}, {"GET", "/stats?_tifa=d1.11"}, {"GET", "/live"},
			{"GET", "/api/health"}, {"POST", "/api/echo"}, {"GET", "/nowhere"}, {"GET", "/assets/gone.js"},
			{"GET", "/__tifa/dev/errors"},
		} {
			fetch(t, req[0], base+req[1])
		}
		return requestLines(logs.String())
	}
	want := []string{
		"GET /stats?tab=2 200 <t> /stats memo miss",
		"GET /stats?tab=2 200 <t> /stats memo hit",
		"GET /stats 200 <t> /stats data",
		"GET /live 200 <t> /live memo off",
		"GET /api/health 200 <t> /api/health",
		"POST /api/echo 204 <t> /api/echo",
		"GET /nowhere 404 <t> memo off",
	}
	got := run(true)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("dev lines\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := run(false); len(got) != 0 {
		t.Errorf("a built app wrote %q", got)
	}
}
