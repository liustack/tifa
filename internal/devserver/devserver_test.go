package devserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa/js"
)

// buildPipeline is a Pipeline for Run: GoBuild writes "build N" to the file the fake app serves.
type buildPipeline struct {
	versionFile string
	mu          sync.Mutex
	calls       []string
	builds      int
	fail        string
}

func (p *buildPipeline) step(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, name)
	if p.fail == name {
		return fmt.Errorf("%s: app/store.go:3:2: undefined: x", strings.ToLower(name))
	}
	if name == "GoBuild" {
		p.builds++
		return os.WriteFile(p.versionFile, []byte(fmt.Sprintf("build %d", p.builds)), 0o644)
	}
	return nil
}

// took returns the steps run since the last call.
func (p *buildPipeline) took() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	calls := p.calls
	p.calls = nil
	return calls
}

func (p *buildPipeline) setFail(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = name
}

func (p *buildPipeline) Analyze(context.Context) (string, error) { return "routes", p.step("Analyze") }
func (p *buildPipeline) Generate(context.Context) error          { return p.step("Generate") }
func (p *buildPipeline) Bundle(context.Context) (string, error)  { return "", p.step("Bundle") }
func (p *buildPipeline) GoBuild(context.Context) error           { return p.step("GoBuild") }

type devRun struct {
	dir    string
	url    string
	p      *buildPipeline
	out    *syncBuffer
	events *eventStream
}

// startDev runs Run on a small app with the fake pipeline and the fake app until the test ends.
func startDev(t *testing.T, failFirst string) *devRun {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "app/store.go", "package app")
	writeFile(t, dir, "app/list/route.tsx", "export {}")
	versionFile := filepath.Join(t.TempDir(), "version")
	t.Setenv("TIFA_FAKE_APP_VERSION", versionFile)
	p := &buildPipeline{versionFile: versionFile, fail: failFirst}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuffer{}
	o := Options{
		Dir:         dir,
		App:         fakeAppCommand(t),
		Pipeline:    p,
		Debounce:    20 * time.Millisecond,
		StopTimeout: 5 * time.Second,
		ProxyWait:   10 * time.Second,
		Stdout:      out,
		Stderr:      out,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, ln, o) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Run did not return within 10s after cancel")
		}
	})
	d := &devRun{dir: dir, url: "http://" + ln.Addr().String(), p: p, out: out}
	d.events = connectEvents(t, d.url+eventsPath)
	return d
}

// waitStatus reads events until one matches.
func (d *devRun) waitStatus(t *testing.T, match func(status) bool) status {
	t.Helper()
	for {
		if st := d.events.next(t); match(st) {
			return st
		}
	}
}

func (d *devRun) waitVersionAfter(t *testing.T, prev string) string {
	t.Helper()
	return d.waitStatus(t, func(st status) bool { return st.Version != prev && st.Error == "" }).Version
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func expectSteps(t *testing.T, p *buildPipeline, want ...string) {
	t.Helper()
	if got := p.took(); !slices.Equal(got, want) {
		t.Fatalf("pipeline ran %v, want %v", got, want)
	}
}

func expectPage(t *testing.T, url, want string) {
	t.Helper()
	if status, body := get(t, url); status != 200 || body != want {
		t.Fatalf("got %d %q, want 200 %q", status, body, want)
	}
}

// expectVersion checks that the page names the build that served it.
func expectVersion(t *testing.T, url, version string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got, want := resp.Header.Get("Server-Timing"), `tifa-dev;desc="`+version+`"`; got != want {
		t.Fatalf("Server-Timing %q, want %q", got, want)
	}
}

func TestRunRebuildsAndKeepsTheOldAppOnFailure(t *testing.T) {
	d := startDev(t, "")
	v1 := d.waitVersionAfter(t, "")
	expectSteps(t, d.p, "Analyze", "Generate", "Bundle", "GoBuild")
	expectPage(t, d.url+"/", "build 1")
	expectVersion(t, d.url+"/", v1)

	writeFile(t, d.dir, "app/store.go", "package app // body edited")
	v2 := d.waitVersionAfter(t, v1)
	expectSteps(t, d.p, "Analyze", "GoBuild")
	expectPage(t, d.url+"/", "build 2")
	expectVersion(t, d.url+"/", v2)

	writeFile(t, d.dir, "app/list/route.tsx", "export const x = 1")
	v3 := d.waitVersionAfter(t, v2)
	expectSteps(t, d.p, "Bundle")
	expectPage(t, d.url+"/", "build 2")

	d.p.setFail("GoBuild")
	writeFile(t, d.dir, "app/store.go", "package app // broken")
	failed := d.waitStatus(t, func(st status) bool { return st.Error != "" })
	want := status{Version: v3, Title: "Build failed", Error: "go build: gobuild: app/store.go:3:2: undefined: x"}
	if failed != want {
		t.Fatalf("got %+v, want %+v", failed, want)
	}
	expectPage(t, d.url+"/", "build 2") // the old app still answers
	if st := connectEvents(t, d.url+eventsPath).next(t); st != want {
		t.Fatalf("a new connection got %+v, want the current error %+v", st, want)
	}

	d.p.setFail("")
	writeFile(t, d.dir, "app/store.go", "package app // fixed")
	d.waitVersionAfter(t, v3)
	expectPage(t, d.url+"/", "build 3")
}

func TestRunReportsTheAppStopping(t *testing.T) {
	d := startDev(t, "")
	v1 := d.waitVersionAfter(t, "")
	if resp, err := http.Get(d.url + "/exit"); err == nil {
		resp.Body.Close() // the proxy answers 502 for the dropped connection
	}
	st := d.waitStatus(t, func(st status) bool { return st.Error != "" })
	if st.Version != v1 || st.Title != "The app stopped" || !strings.Contains(st.Error, "exit status 3") {
		t.Fatalf("got %+v", st)
	}
	// With no app to wait for, the proxy answers at once with the error.
	if status, body := get(t, d.url+"/"); status != http.StatusBadGateway || !strings.Contains(body, "exit status 3") {
		t.Fatalf("got %d %q", status, body)
	}
	writeFile(t, d.dir, "app/store.go", "package app // restart me")
	d.waitVersionAfter(t, v1)
	expectPage(t, d.url+"/", "build 2")
}

func TestRunRecoversFromAFailedFirstBuild(t *testing.T) {
	d := startDev(t, "Analyze")
	st := d.waitStatus(t, func(st status) bool { return st.Error != "" })
	if st.Version != "" || !strings.HasPrefix(st.Error, "static analysis: ") {
		t.Fatalf("got %+v", st)
	}
	if status, body := get(t, d.url+"/"); status != http.StatusBadGateway || !strings.Contains(body, "undefined: x") {
		t.Fatalf("got %d %q, want the build error at once", status, body)
	}
	d.p.setFail("")
	writeFile(t, d.dir, "app/store.go", "package app // fixed")
	d.waitVersionAfter(t, "")
	expectPage(t, d.url+"/", "build 1")
}

func TestRunStopsTheAppWithAnInterrupt(t *testing.T) {
	var d *devRun
	t.Run("run", func(t *testing.T) {
		d = startDev(t, "")
		d.waitVersionAfter(t, "")
	}) // the subtest's cleanup stops Run
	if !strings.Contains(d.out.String(), "shutdown done") {
		t.Fatalf("the app did not shut down cleanly, output %q", d.out.String())
	}
}

func TestRunServesTheDevScripts(t *testing.T) {
	d := startDev(t, "")
	for path, want := range map[string]string{clientPath: js.DevClient, workerPath: js.DevWorker} {
		resp, err := http.Get(d.url + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if path == clientPath && !strings.HasPrefix(string(body), "window.__tifaDevServed = \"") {
			t.Errorf("client.js does not start with the served version: %.60q", body)
		}
		if !strings.HasSuffix(string(body), want) || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript") {
			t.Errorf("%s: got %s with %d bytes", path, resp.Header.Get("Content-Type"), len(body))
		}
	}
}

func TestRunRefusesIncompleteOptions(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	err = Run(context.Background(), ln, Options{Dir: t.TempDir()})
	if err == nil || !errors.Is(err, errOptions) {
		t.Fatalf("got %v, want an options error", err)
	}
}

func TestRunStopsWhenServingFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close() // Serve fails at once
	done := make(chan error, 1)
	go func() {
		done <- Run(t.Context(), ln, Options{Dir: t.TempDir(), App: []string{"unused"},
			Pipeline: &fakePipeline{fail: "Analyze"}, Stdout: io.Discard, Stderr: io.Discard})
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "serving") {
			t.Fatalf("got %v, want the serve error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept running after the proxy stopped serving")
	}
}
