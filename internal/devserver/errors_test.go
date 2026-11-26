package devserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/deverrors"
)

// report asks tifa dev for its errors, as `tifa errors` does.
func report(t *testing.T, url string) deverrors.Report {
	t.Helper()
	code, body := get(t, url+deverrors.Path)
	if code != 200 {
		t.Fatalf("GET %s: %d %s", deverrors.Path, code, body)
	}
	var r deverrors.Report
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("GET %s: %v\n%s", deverrors.Path, err, body)
	}
	return r
}

// sendBrowserError posts an error the way the dev client of a page does, from origin, and
// returns the status of the answer.
func sendBrowserError(t *testing.T, url, origin, host, body string) int {
	t.Helper()
	req, err := http.NewRequest("POST", url+deverrors.Path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

const hydration = `{"kind":"recoverable","title":"Hydration mismatch","message":"Hydration failed","stack":"Error: Hydration failed\n    at a","text":"Error: Hydration failed\n    at a","url":"http://127.0.0.1/orders"}`

// tifa dev answers its errors: the request errors of the app, and the errors its pages report,
// with a count for repeats. Everything is empty once the app runs.
func TestErrorsReportsTheAppAndTheBrowser(t *testing.T) {
	d := startDev(t, "")
	d.waitVersionAfter(t, "")
	if r := report(t, d.url); !r.Empty() {
		t.Fatalf("report of a fresh app: %+v", r)
	}
	code, body := get(t, d.url+deverrors.Path)
	if code != 200 || body != `{"failure":null,"requests":[],"browser":[],"renders":[],"warnings":[]}`+"\n" {
		t.Fatalf("empty report: %d %q", code, body)
	}
	get(t, d.url+"/fail")
	for range 2 {
		if code := sendBrowserError(t, d.url, d.url, "", hydration); code != http.StatusNoContent {
			t.Fatalf("report from the page: %d", code)
		}
	}
	r := report(t, d.url)
	if len(r.Requests) != 1 || r.Requests[0].URL != "/fail" || r.Requests[0].Error != "the disk is full" {
		t.Errorf("requests = %+v", r.Requests)
	}
	want := deverrors.Browser{Kind: "recoverable", Title: "Hydration mismatch", URL: "http://127.0.0.1/orders", Text: "Error: Hydration failed\n    at a", Count: 2}
	if len(r.Browser) != 1 || r.Browser[0] != want {
		t.Errorf("browser = %+v", r.Browser)
	}
	if r.Failure != nil {
		t.Errorf("failure = %+v", r.Failure)
	}
}

// Only the pages of tifa dev can report: a page of another origin, a request without an origin,
// a name that is not localhost or an address, which DNS rebinding would use, and a report that
// is too large, or that does not parse, are refused.
func TestErrorsTakesReportsOnlyFromItsPages(t *testing.T) {
	d := startDev(t, "")
	d.waitVersionAfter(t, "")
	host := strings.TrimPrefix(d.url, "http://")
	port := host[strings.LastIndex(host, ":"):]
	for _, tc := range []struct {
		name, origin, host, body string
		want                     int
	}{
		{"another site", "http://evil.test", "", hydration, http.StatusForbidden},
		{"another port", "http://127.0.0.1:1", "", hydration, http.StatusForbidden},
		{"no origin", "", "", hydration, http.StatusForbidden},
		{"a domain name", "http://evil.test" + port, "evil.test" + port, hydration, http.StatusForbidden},
		{"too large", d.url, "", `{"kind":"uncaught","text":"` + strings.Repeat("x", maxBrowserReport) + `"}`, http.StatusRequestEntityTooLarge},
		{"not JSON", d.url, "", "boom", http.StatusBadRequest},
		{"no kind", d.url, "", `{"text":"x"}`, http.StatusBadRequest},
		// The whole body counts: a second value or a long tail after the report is refused too.
		{"a second value", d.url, "", `{"kind":"uncaught","text":"x"} {"kind":"uncaught"}`, http.StatusBadRequest},
		{"a long tail", d.url, "", `{"kind":"uncaught","text":"x"}` + strings.Repeat(" ", maxBrowserReport), http.StatusRequestEntityTooLarge},
		{"localhost", "http://localhost" + port, "localhost" + port, hydration, http.StatusNoContent},
	} {
		if got := sendBrowserError(t, d.url, tc.origin, tc.host, tc.body); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
	if r := report(t, d.url); len(r.Browser) != 1 {
		t.Fatalf("browser = %+v", r.Browser)
	}
	// Reading the errors needs an address or localhost too.
	req, err := http.NewRequest("GET", d.url+deverrors.Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.test" + port
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET under a domain name: %d", resp.StatusCode)
	}
}

// A build forgets the errors from before it starts: those of the browser and those of the app.
// A failed build shows its message until the next build, also once the app stopped.
func TestErrorsStartOverWithEachBuild(t *testing.T) {
	d := startDev(t, "")
	v1 := d.waitVersionAfter(t, "")
	get(t, d.url+"/fail")
	sendBrowserError(t, d.url, d.url, "", hydration)

	d.p.setFail("GoBuild")
	writeFile(t, d.dir, "app/store.go", "package app // broken")
	d.waitStatus(t, func(st status) bool { return st.Error != "" })
	r := report(t, d.url)
	if r.Failure == nil || r.Failure.Title != "Build failed" || r.Failure.Error != "go build: gobuild: app/store.go:3:2: undefined: x" {
		t.Fatalf("failure = %+v", r.Failure)
	}
	if len(r.Requests) != 0 || len(r.Browser) != 0 {
		t.Fatalf("errors from before the build: %+v", r)
	}
	// The old app still serves, and its errors count.
	get(t, d.url+"/fail")
	if r := report(t, d.url); len(r.Requests) != 1 || r.Failure == nil {
		t.Fatalf("after the failed build: %+v", r)
	}

	d.p.setFail("")
	writeFile(t, d.dir, "app/store.go", "package app // fixed")
	d.waitVersionAfter(t, v1)
	if r := report(t, d.url); !r.Empty() {
		t.Fatalf("after the fix: %+v", r)
	}

	// An app that stops is a failure too.
	get(t, d.url+"/exit")
	d.waitStatus(t, func(st status) bool { return st.Error != "" })
	if r := report(t, d.url); r.Failure == nil || r.Failure.Title != "The app stopped" || !strings.Contains(r.Failure.Error, "the disk is full") {
		t.Fatalf("after the app stopped: %+v", r.Failure)
	}
}

// A report asked for during a build waits for the build to end, so it shows what the build did.
func TestErrorsWaitsForTheBuild(t *testing.T) {
	d := startDev(t, "")
	d.waitVersionAfter(t, "")
	bundling := make(chan chan struct{})
	d.p.mu.Lock()
	d.p.bundling = bundling
	d.p.broken = true
	d.p.mu.Unlock()
	writeFile(t, d.dir, "app/list/route.tsx", "export const x = 2")
	var release chan struct{}
	select {
	case release = <-bundling:
	case <-time.After(5 * time.Second):
		t.Fatal("no bundle within 5s")
	}
	answered := make(chan deverrors.Report, 1)
	go func() {
		var r deverrors.Report
		resp, err := http.Get(d.url + deverrors.Path)
		if err == nil {
			err = json.NewDecoder(resp.Body).Decode(&r)
			resp.Body.Close()
		}
		if err != nil {
			r.Failure = &deverrors.Failure{Title: "request", Error: err.Error()}
		}
		answered <- r
	}()
	select {
	case r := <-answered:
		t.Fatalf("a report during the build came before it ended: %+v", r)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case r := <-answered:
		if r.Failure == nil || r.Failure.Title != "The app did not load the SSR bundle" {
			t.Fatalf("report after the build: %+v", r.Failure)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no report within 5s after the build")
	}
}

// A build that starts while the report is put together drops what the report read, so the report
// waits for that build and reads again: it never answers with the errors of a build that a newer
// one already dropped.
func TestErrorsReadsAgainAfterABuildStarts(t *testing.T) {
	d := startDev(t, "")
	d.waitVersionAfter(t, "")
	bundling := make(chan chan struct{})
	d.p.mu.Lock()
	d.p.bundling = bundling
	d.p.broken = true
	d.p.mu.Unlock()
	once := false
	testHookCollectErrors = func() {
		if once {
			return
		}
		once = true
		// A change starts a build, which drops the errors, before the report is read.
		writeFile(t, d.dir, "app/list/route.tsx", "export const x = 3")
		release := <-bundling
		go func() {
			time.Sleep(100 * time.Millisecond)
			close(release)
		}()
	}
	t.Cleanup(func() { testHookCollectErrors = nil })
	r := report(t, d.url)
	if r.Failure == nil || r.Failure.Title != "The app did not load the SSR bundle" {
		t.Fatalf("report = %+v, want the failure of the build that started while it was read", r)
	}
}

// status asks tifa dev for the state of its builds, as `tifa dev status` does.
func askStatus(t *testing.T, url string) (deverrors.Status, string) {
	t.Helper()
	resp, err := http.Get(url + deverrors.StatusPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st deverrors.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("GET %s: %v", deverrors.StatusPath, err)
	}
	return st, resp.Header.Get(deverrors.PIDHeader)
}

// The state answers at once: during a build it says so and does not wait, and a failure comes with
// its error.
func TestStatusAnswersAtOnce(t *testing.T) {
	d := startDev(t, "")
	d.waitVersionAfter(t, "")
	st, pid := askStatus(t, d.url)
	if st.State != deverrors.OK || st.Failure != nil || pid == "" {
		t.Fatalf("running: %+v, pid %q", st, pid)
	}
	// The process ID of the app, which a debugger attaches to, is the one that listens.
	if st.AppPID <= 0 || strconv.Itoa(st.AppPID) == pid {
		t.Fatalf("the app's process ID is %d, tifa dev's is %s", st.AppPID, pid)
	}

	bundling := make(chan chan struct{})
	d.p.mu.Lock()
	d.p.bundling = bundling
	d.p.mu.Unlock()
	writeFile(t, d.dir, "app/list/route.tsx", "export const x = 2")
	var release chan struct{}
	select {
	case release = <-bundling:
	case <-time.After(5 * time.Second):
		t.Fatal("no bundle within 5s")
	}
	if st, _ := askStatus(t, d.url); st.State != deverrors.Building {
		t.Fatalf("during a build: %+v", st)
	}
	close(release)
	d.waitVersionAfter(t, "")

	d.p.setFail("GoBuild")
	writeFile(t, d.dir, "app/store.go", "package app // broken")
	d.waitStatus(t, func(st status) bool { return st.Error != "" })
	st, _ = askStatus(t, d.url)
	if st.State != deverrors.Failed || st.Failure == nil || st.Failure.Title != "Build failed" || !strings.Contains(st.Failure.Error, "undefined: x") {
		t.Fatalf("after a failed build: %+v", st)
	}
}

// The warnings of the last analysis, the idiom checks and the content are in the report, and they
// are not errors: a report with only warnings is empty.
func TestErrorsListsTheWarningsOfTheBuild(t *testing.T) {
	d := startDev(t, "", func(p *buildPipeline) { p.warnings = []string{"app/a/route.go:3:5: a big number (warning)"} })
	d.waitVersionAfter(t, "")
	r := report(t, d.url)
	if len(r.Warnings) != 1 || r.Warnings[0] != "app/a/route.go:3:5: a big number (warning)" {
		t.Errorf("warnings = %q", r.Warnings)
	}
	if !r.Empty() {
		t.Errorf("a warning made the report non-empty: %+v", r)
	}
}
