package devserver

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/liustack/tifa/js"
)

// buildPipeline is a Pipeline for Run: GoBuild writes "build N" to the file the fake app serves,
// Bundle writes "bundle N" to the file the fake app loads as its SSR bundle, or "broken".
type buildPipeline struct {
	versionFile string
	bundleFile  string
	bundles     int
	broken      bool
	mu          sync.Mutex
	calls       []string
	builds      int
	fail        string
	// bundling, when set, gets a channel each Bundle waits on until the test closes it.
	bundling chan chan struct{}
	manifest string // what Analyze returns, "routes" when empty
}

func (p *buildPipeline) step(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, name)
	if p.fail == name {
		return fmt.Errorf("%s: app/store.go:3:2: undefined: x", strings.ToLower(name))
	}
	switch name {
	case "GoBuild":
		p.builds++
		return os.WriteFile(p.versionFile, []byte(fmt.Sprintf("build %d", p.builds)), 0o644)
	case "Bundle":
		p.bundles++
		bundle := fmt.Sprintf("bundle %d", p.bundles)
		if p.broken {
			bundle = "broken"
		}
		return os.WriteFile(p.bundleFile, []byte(bundle), 0o644)
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

func (p *buildPipeline) Analyze(context.Context) (string, error) {
	p.mu.Lock()
	manifest := cmp.Or(p.manifest, "routes")
	p.mu.Unlock()
	return manifest, p.step("Analyze")
}
func (p *buildPipeline) Generate(context.Context) error { return p.step("Generate") }
func (p *buildPipeline) Bundle(context.Context) (string, error) {
	p.mu.Lock()
	bundling := p.bundling
	p.mu.Unlock()
	if bundling != nil {
		release := make(chan struct{})
		bundling <- release
		<-release
	}
	return "", p.step("Bundle")
}
func (p *buildPipeline) GoBuild(context.Context) error { return p.step("GoBuild") }
func (p *buildPipeline) PollFiles(context.Context) (bool, error) {
	return true, p.step("PollFiles")
}

type devRun struct {
	dir     string
	url     string
	p       *buildPipeline
	out     *syncBuffer
	events  *eventStream
	outside *Outside // Options.Outside
}

// startDev runs Run on a small app with the fake pipeline and the fake app until the test ends.
func startDev(t *testing.T, failFirst string) *devRun {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "app/store.go", "package app")
	writeFile(t, dir, "app/list/route.tsx", "export {}")
	versionFile := filepath.Join(t.TempDir(), "version")
	t.Setenv("TIFA_FAKE_APP_VERSION", versionFile)
	bundleFile := filepath.Join(t.TempDir(), "bundle")
	t.Setenv("TIFA_FAKE_APP_BUNDLE", bundleFile)
	p := &buildPipeline{versionFile: versionFile, bundleFile: bundleFile, fail: failFirst}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuffer{}
	outside := &Outside{}
	o := Options{
		Outside:     outside,
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
	d := &devRun{dir: dir, url: "http://" + ln.Addr().String(), p: p, out: out, outside: outside}
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

	// Vite updates the open pages after a front end change, so the version stays and they do not
	// reload. Only the server render changes.
	writeFile(t, d.dir, "app/list/route.tsx", "export const x = 1")
	d.waitStatus(t, func(st status) bool { return st.Version == v2 && st.Error == "" })
	expectSteps(t, d.p, "Bundle")
	expectPage(t, d.url+"/", "build 2")
	v3 := v2

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

// A page requested while the SSR bundle is rebuilt waits for the app that serves the new one, so
// it never shows the old server render.
func TestRunHoldsPagesWhileBundling(t *testing.T) {
	d := startDev(t, "")
	v1 := d.waitVersionAfter(t, "")
	d.p.took()
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
	answered := make(chan string, 1)
	go func() {
		resp, err := http.Get(d.url + "/bundle")
		if err != nil {
			answered <- err.Error()
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			answered <- err.Error()
			return
		}
		answered <- string(body)
	}()
	select {
	case got := <-answered:
		t.Fatalf("a request during the bundle step got %q before the bundle finished", got)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case got := <-answered:
		if got != "bundle 2" {
			t.Fatalf("the held request got %q, want the new bundle", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the request still waits 5s after the bundle")
	}
	d.waitStatus(t, func(st status) bool { return st.Version == v1 && st.Error == "" })
	expectSteps(t, d.p, "Bundle")
}

// A Go change whose analysis finds a new route manifest rebuilds the SSR bundle too. Page
// requests wait for it as soon as the bundle step is known, as they do for a front end change.
func TestRunHoldsPagesWhileTheAnalysisAddsABundle(t *testing.T) {
	d := startDev(t, "")
	d.waitVersionAfter(t, "")
	d.p.took()
	bundling := make(chan chan struct{})
	d.p.mu.Lock()
	d.p.bundling = bundling
	d.p.manifest = "routes 2"
	d.p.mu.Unlock()
	writeFile(t, d.dir, "app/store.go", "package app // a new route")
	var release chan struct{}
	select {
	case release = <-bundling:
	case <-time.After(5 * time.Second):
		t.Fatal("no bundle within 5s")
	}
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	answered := make(chan string, 1)
	go func() {
		resp, err := http.Get(d.url + "/")
		if err != nil {
			answered <- err.Error()
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		answered <- string(body)
	}()
	select {
	case got := <-answered:
		t.Fatalf("a request during the bundle step got %q before the rebuild finished", got)
	case <-time.After(200 * time.Millisecond):
	}
	free()
	select {
	case got := <-answered:
		if got != "build 2" {
			t.Fatalf("the held request got %q, want the new build", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the request still waits 5s after the bundle")
	}
	expectSteps(t, d.p, "Analyze", "Generate", "Bundle", "GoBuild")
}

// A file outside the app that Vite's dev server watches, such as a package of the workspace,
// arrives through Options.Outside and rebuilds the SSR bundle. Vite watches it because a module
// loaded it, so it does whatever its name: also a text file imported with ?raw.
func TestRunRebuildsForFilesOutsideTheApp(t *testing.T) {
	d := startDev(t, "")
	v1 := d.waitVersionAfter(t, "")
	d.p.took()
	parent, err := filepath.EvalSymlinks(filepath.Dir(d.dir)) // Vite names files by their real path
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"card.tsx", "value.txt"} {
		d.outside.Changed(filepath.Join(parent, "shared", name))
		d.waitStatus(t, func(st status) bool { return st.Version == v1 && st.Error == "" })
		expectSteps(t, d.p, "Bundle")
		if !strings.Contains(d.out.String(), "../shared/"+name+" modified") {
			t.Fatalf("the log does not name the file:\n%s", d.out.String())
		}
	}
}

// When the system runs out of watches, tifa dev polls and tells Vite's dev server to poll too.
func TestRunTellsViteToPollWhenOutOfWatches(t *testing.T) {
	outErr := outOfWatchesErrors(t)[0]
	newFSWatcher = func() (*fsnotify.Watcher, error) { return nil, outErr }
	defer func() { newFSWatcher = fsnotify.NewWatcher }()
	d := startDev(t, "")
	d.waitVersionAfter(t, "")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(d.out.String(), "Vite's dev server polls the files too") {
		if time.Now().After(deadline) {
			t.Fatalf("Vite's dev server was not told to poll:\n%s", d.out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls := d.p.took(); !slices.Contains(calls, "PollFiles") {
		t.Fatalf("pipeline ran %v, want PollFiles among them", calls)
	}
}

// After a rebuild of the SSR bundle alone, the running app loads the new bundle: no restart, so
// OnStart does not run again and its state stays. A bundle the app cannot load shows the error,
// and the app keeps serving the bundle it has.
func TestRunReloadsTheBundleWithoutARestart(t *testing.T) {
	d := startDev(t, "")
	v1 := d.waitVersionAfter(t, "")
	d.p.took()
	_, pid := get(t, d.url+"/pid")
	expectPage(t, d.url+"/bundle", "bundle 1")

	writeFile(t, d.dir, "app/list/route.tsx", "export const x = 2")
	d.waitStatus(t, func(st status) bool { return st.Version == v1 && st.Error == "" })
	expectSteps(t, d.p, "Bundle")
	expectPage(t, d.url+"/bundle", "bundle 2")
	expectPage(t, d.url+"/pid", pid)

	d.p.mu.Lock()
	d.p.broken = true
	d.p.mu.Unlock()
	writeFile(t, d.dir, "app/list/route.tsx", "export const x = 3")
	failed := d.waitStatus(t, func(st status) bool { return st.Error != "" })
	if failed.Version != v1 || failed.Title != "The app did not load the SSR bundle" || !strings.Contains(failed.Error, "SyntaxError: the bundle is broken") {
		t.Fatalf("got %+v", failed)
	}
	expectPage(t, d.url+"/bundle", "bundle 2")
	expectPage(t, d.url+"/pid", pid)

	d.p.mu.Lock()
	d.p.broken = false
	d.p.mu.Unlock()
	writeFile(t, d.dir, "app/list/route.tsx", "export const x = 4")
	d.waitStatus(t, func(st status) bool { return st.Version == v1 && st.Error == "" })
	expectPage(t, d.url+"/bundle", "bundle 4")
	expectPage(t, d.url+"/pid", pid)
	if strings.Contains(d.out.String(), "shutdown done") {
		t.Fatalf("the app was restarted:\n%s", d.out.String())
	}
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

// blockingPipeline is a buildPipeline whose step named block waits until the test releases it,
// then fails, which ends the rebuild.
type blockingPipeline struct {
	buildPipeline
	block   string
	entered chan struct{}
	release chan struct{}
}

func (p *blockingPipeline) wait(name string) error {
	if name != p.block {
		return nil
	}
	close(p.entered)
	<-p.release
	return errors.New("released")
}

func (p *blockingPipeline) Analyze(ctx context.Context) (string, error) {
	if err := p.wait("Analyze"); err != nil {
		return "", err
	}
	return p.buildPipeline.Analyze(ctx)
}

func (p *blockingPipeline) Generate(ctx context.Context) error {
	if err := p.wait("Generate"); err != nil {
		return err
	}
	return p.buildPipeline.Generate(ctx)
}

// Page requests wait from the moment a rebuild is known to make a new SSR bundle: once the
// analysis finds a new route manifest, before code generation, and from the start for a bundle
// that a failed rebuild left undone.
func TestRebuildHoldsPagesOnceABundleIsNeeded(t *testing.T) {
	for _, c := range []struct {
		name  string
		block string
		setup func(d *dev, p *blockingPipeline)
	}{
		{"new manifest", "Generate", func(d *dev, p *blockingPipeline) { p.manifest = "routes 2" }},
		{"bundle left undone", "Analyze", func(d *dev, p *blockingPipeline) {
			p.fail = "Bundle"
			d.rebuild(context.Background(), steps{bundle: true})
			p.fail = ""
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &blockingPipeline{block: c.block, entered: make(chan struct{}), release: make(chan struct{})}
			p.versionFile = filepath.Join(t.TempDir(), "version")
			p.bundleFile = filepath.Join(t.TempDir(), "bundle")
			d := &dev{b: builder{p: p, manifest: "routes", log: func(string, ...any) {}}, hub: newHub(), log: log.New(io.Discard, "", 0)}
			c.setup(d, p)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				d.rebuild(ctx, steps{analyze: true, goBuild: true})
				close(done)
			}()
			<-p.entered
			busy := d.busy()
			cancel()
			close(p.release)
			<-done
			if busy == nil {
				t.Fatalf("pages do not wait during %s, though the rebuild makes a new SSR bundle", c.block)
			}
		})
	}
}
