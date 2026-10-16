package e2e

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// devApp copies test/apps/pages into a temporary directory that tifa dev may edit.
func devApp(t *testing.T) string {
	t.Helper()
	return copyApp(t, "pages")
}

// copyApp copies test/apps/<name> into a temporary directory that tifa dev may edit. go.mod points
// at this repository and node_modules is linked from the original, which must be installed.
func copyApp(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "test", "apps", name)
	if _, err := os.Stat(filepath.Join(src, "node_modules")); err != nil {
		t.Skipf("run pnpm install --frozen-lockfile in test/apps/%s first", name)
	}
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if slices.Contains([]string{"node_modules", ".tifa", "bin"}, e.Name()) {
			continue
		}
		if out, err := exec.Command("cp", "-R", filepath.Join(src, e.Name()), dst).CombinedOutput(); err != nil {
			t.Fatalf("copy %s: %v\n%s", e.Name(), err, out)
		}
	}
	if err := os.Symlink(filepath.Join(src, "node_modules"), filepath.Join(dst, "node_modules")); err != nil {
		t.Fatal(err)
	}
	gomod := filepath.Join(dst, "go.mod")
	data, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "=> ../../..", "=> "+repoRoot(t), 1))
	if err := os.WriteFile(gomod, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

type devRun struct {
	dir      string
	base     string
	log      *lockedBuffer
	statuses chan devStatus
	exited   chan error // gets how tifa dev exited
	stopped  bool       // tifa dev is expected to exit on its own, with an error
}

type devStatus struct {
	Version string `json:"version"`
	Render  int    `json:"render"`
	Title   string `json:"title"`
	Error   string `json:"error"`
}

var serving = regexp.MustCompile(`tifa dev: serving (http://\S+)`)

// startDev runs tifa dev on dir and follows its SSE events.
func startDev(t *testing.T, dir string) *devRun {
	t.Helper()
	d := &devRun{dir: dir, log: &lockedBuffer{}, statuses: make(chan devStatus, 100), exited: make(chan error, 1)}
	cmd := exec.Command(cli(t), "dev", "--dir", dir, "--addr", "127.0.0.1:0")
	// The app rejects unknown TIFA_* variables as settings, TIFA_E2E among them.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TIFA_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Stdout = d.log
	cmd.Stderr = d.log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		d.exited <- err
		done <- err
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGINT)
		select {
		case err := <-done:
			if err != nil && !d.stopped {
				t.Errorf("tifa dev exited with %v\n%s", err, d.log)
			}
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
			t.Errorf("tifa dev did not stop after SIGINT\n%s", d.log)
		}
	})
	d.waitLog(t, 0, serving)
	d.base = serving.FindStringSubmatch(d.log.String())[1]

	resp, err := http.Get(d.base + "/__tifa/dev/events")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
				var st devStatus
				if json.Unmarshal([]byte(data), &st) == nil {
					d.statuses <- st
				}
			}
		}
	}()
	return d
}

// waitLog waits until the log after offset from matches re and returns the log from there.
func (d *devRun) waitLog(t *testing.T, from int, re *regexp.Regexp) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if out := d.log.String()[from:]; re.MatchString(out) {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("log never matched %s:\n%s", re, d.log)
	return ""
}

// waitStatus returns the first status that matches.
func (d *devRun) waitStatus(t *testing.T, match func(devStatus) bool) devStatus {
	t.Helper()
	timeout := time.After(2 * time.Minute)
	for {
		select {
		case st := <-d.statuses:
			if match(st) {
				return st
			}
		case <-timeout:
			t.Fatalf("no matching status\n%s", d.log)
		}
	}
}

// edit replaces old with new in a file of the app and returns the log offset before the edit.
func (d *devRun) edit(t *testing.T, name, old, new string) int {
	t.Helper()
	path := filepath.Join(d.dir, filepath.FromSlash(name))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s lacks %q", name, old)
	}
	mark := len(d.log.String())
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return mark
}

var (
	ready       = regexp.MustCompile(`ready in \S+`)
	buildFailed = regexp.MustCompile(`build failed: `)
	stepDone    = regexp.MustCompile(`(static analysis|code generation|bundle|go build) done in (\S+)`)
)

// steps lists the steps logged in out, and their durations.
func steps(out string) (names []string, took []string) {
	for _, m := range stepDone.FindAllStringSubmatch(out, -1) {
		names = append(names, m[1])
		took = append(took, m[2])
	}
	return names, took
}

func expectSteps(t *testing.T, change, out string, want ...string) {
	t.Helper()
	names, took := steps(out)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("%s ran %v, want %v:\n%s", change, names, want, out)
	}
	t.Logf("%s: %v %v, %s", change, names, took, ready.FindString(out))
}

func expectBody(t *testing.T, url, want string) {
	t.Helper()
	status, _, body := get(t, url)
	if status != 200 || !strings.Contains(body, want) {
		t.Fatalf("GET %s: %d, want 200 with %q:\n%.400s", url, status, want, body)
	}
}

func TestDev(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	d := startDev(t, devApp(t))

	// The first start runs every step.
	out := d.waitLog(t, 0, ready)
	expectSteps(t, "first build", out, "static analysis", "code generation", "bundle", "go build")
	v1 := d.waitStatus(t, func(st devStatus) bool { return st.Version != "" && st.Error == "" }).Version
	expectBody(t, d.base+"/", "North shop")

	// Pages load the modules from Vite's dev server, with the React refresh preamble first.
	expectBody(t, d.base+"/", `import { injectIntoGlobalHook } from "/__tifa/vite/@react-refresh"`)
	expectBody(t, d.base+"/", `<script type="module" src="/__tifa/vite/@vite/client"></script>`)
	expectBody(t, d.base+"/", `<script type="module" src="/__tifa/vite/.tifa/client.ts"></script>`)
	hmr := dialHMR(t, d.base)
	// Vite updates the modules a page loaded. Load them the way the page does.
	for _, module := range []string{".tifa/client.ts", ".tifa/routes.tsx", "app/route.tsx"} {
		expectBody(t, d.base+"/__tifa/vite/"+module, "import")
	}

	// TSX only: Vite updates the open pages in place, so the version stays and they do not
	// reload. tifa rebuilds the SSR bundle and restarts the app, without go build.
	mark := d.edit(t, "app/route.tsx", "North shop", "North store")
	hmr.waitUpdate(t, "/app/route.tsx")
	expectSteps(t, "TSX edit", d.waitLog(t, mark, ready), "bundle")
	r2 := d.waitStatus(t, func(st devStatus) bool { return st.Version == v1 && st.Error == "" }).Render
	expectBody(t, d.base+"/", "North store")
	expectBody(t, d.base+"/__tifa/vite/app/route.tsx", "North store")
	v2 := v1

	// A component that throws while the page renders on the server: the page shows the error
	// with its place in the source and loads the dev client. The fix keeps the version and
	// changes the count of builds served, which makes that page reload.
	broken := "export default function Home() {\n  if (typeof window === 'undefined') throw new Error('home is broken')\n"
	mark = d.edit(t, "app/route.tsx", "export default function Home() {\n", broken)
	d.waitLog(t, mark, ready)
	before := d.waitStatus(t, func(st devStatus) bool { return st.Version == v2 && st.Render > r2 && st.Error == "" }).Render
	status, _, body := get(t, d.base+"/")
	for _, want := range []string{"home is broken", "app/route.tsx", "GET / failed while rendering on the server.", `<script src="/__tifa/dev/client.js"></script>`} {
		if status != 500 || !strings.Contains(body, want) {
			t.Fatalf("a throwing component: %d, want 500 with %q:\n%.2000s", status, want, body)
		}
	}
	mark = d.edit(t, "app/route.tsx", broken, "export default function Home() {\n")
	d.waitLog(t, mark, ready)
	d.waitStatus(t, func(st devStatus) bool { return st.Version == v2 && st.Render > before && st.Error == "" })
	expectBody(t, d.base+"/", "North store")

	// A Go function body: analysis finds the same routes, go build without a bundle.
	mark = d.edit(t, "app/shops/shop+/layout.go", `"Shop " + id`, `"Store " + id`)
	out = d.waitLog(t, mark, ready)
	expectSteps(t, "Go body edit", out, "static analysis", "go build")
	if !strings.Contains(out, "route manifest unchanged") {
		t.Errorf("Go body edit changed the manifest:\n%s", out)
	}
	v3 := d.waitStatus(t, func(st devStatus) bool { return st.Version != v2 && st.Error == "" }).Version
	expectBody(t, d.base+"/shops/north", "Store north")

	// A changed SSR constant changes the route manifest: everything runs.
	mark = d.edit(t, "app/account/login/route.go", "const SSR = false", "const SSR = true")
	out = d.waitLog(t, mark, ready)
	expectSteps(t, "SSR edit", out, "static analysis", "code generation", "bundle", "go build")
	if !strings.Contains(out, "route manifest changed") {
		t.Errorf("SSR edit kept the manifest:\n%s", out)
	}
	v4 := d.waitStatus(t, func(st devStatus) bool { return st.Version != v3 && st.Error == "" }).Version
	if _, header, _ := get(t, d.base+"/account/login"); header.Get("X-Tifa-Render") != "vm" {
		t.Errorf("SSR edit: rendered by %q", header.Get("X-Tifa-Render"))
	}

	// A broken build keeps the running app and reports the error over SSE.
	mark = d.edit(t, "app/shops/shop+/layout.go", `return Shop{ID: id, Name: "Store " + id}, nil`, `return Shop{ID: id, Name: "Store " + id}, nil +`)
	d.waitLog(t, mark, buildFailed)
	failed := d.waitStatus(t, func(st devStatus) bool { return st.Error != "" })
	if failed.Version != v4 || failed.Title != "Build failed" || !strings.Contains(failed.Error, "layout.go") {
		t.Fatalf("status %+v", failed)
	}
	expectBody(t, d.base+"/shops/north", "Store north")

	// The fix clears the error and brings a new version.
	mark = d.edit(t, "app/shops/shop+/layout.go", `return Shop{ID: id, Name: "Store " + id}, nil +`, `return Shop{ID: id, Name: "Shop " + id + "!"}, nil`)
	d.waitLog(t, mark, ready)
	d.waitStatus(t, func(st devStatus) bool { return st.Version != v4 && st.Error == "" })
	expectBody(t, d.base+"/shops/north", "Shop north!")
}

// hmrSocket is a client of Vite's HMR websocket, reached through tifa dev.
type hmrSocket struct {
	conn net.Conn
	r    *bufio.Reader
}

var wsToken = regexp.MustCompile(`const wsToken = "([^"]+)"`)

// dialHMR connects to Vite's HMR websocket the way the Vite client in the page does, and waits
// for Vite to say it is connected.
func dialHMR(t *testing.T, base string) *hmrSocket {
	t.Helper()
	_, _, client := get(t, base+"/__tifa/vite/@vite/client")
	token := wsToken.FindStringSubmatch(client)
	if token == nil {
		t.Fatalf("no websocket token in the Vite client:\n%.400s", client)
	}
	host := strings.TrimPrefix(base, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(conn, "GET /__tifa/vite/?token=%s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n"+
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Protocol: vite-hmr\r\n\r\n",
		url.QueryEscape(token[1]), host, base64.StdEncoding.EncodeToString(key))
	s := &hmrSocket{conn: conn, r: bufio.NewReader(conn)}
	resp, err := http.ReadResponse(s.r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("HMR websocket: %s", resp.Status)
	}
	if msg := s.next(t); !strings.Contains(msg, `"type":"connected"`) {
		t.Fatalf("first HMR message %s", msg)
	}
	return s
}

// next returns the next text message. Vite sends unmasked frames without extensions.
func (s *hmrSocket) next(t *testing.T) string {
	t.Helper()
	_ = s.conn.SetReadDeadline(time.Now().Add(time.Minute))
	var message []byte
	for {
		head := make([]byte, 2)
		if _, err := io.ReadFull(s.r, head); err != nil {
			t.Fatalf("HMR websocket: %v", err)
		}
		size := uint64(head[1] & 0x7f)
		switch size {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(s.r, ext); err != nil {
				t.Fatal(err)
			}
			size = uint64(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(s.r, ext); err != nil {
				t.Fatal(err)
			}
			size = binary.BigEndian.Uint64(ext)
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(s.r, payload); err != nil {
			t.Fatal(err)
		}
		switch head[0] & 0x0f {
		case 0x8:
			t.Fatalf("Vite closed the HMR websocket: %q", payload)
		case 0x9, 0xa: // ping and pong
			continue
		}
		message = append(message, payload...)
		if head[0]&0x80 != 0 {
			return string(message)
		}
	}
}

// waitUpdate waits for Vite to send an update of the module at path. A full reload fails.
func (s *hmrSocket) waitUpdate(t *testing.T, path string) {
	t.Helper()
	for {
		msg := s.next(t)
		var payload struct {
			Type    string `json:"type"`
			Updates []struct {
				Path string `json:"path"`
			} `json:"updates"`
		}
		if err := json.Unmarshal([]byte(msg), &payload); err != nil {
			t.Fatalf("HMR message %s: %v", msg, err)
		}
		switch payload.Type {
		case "full-reload":
			t.Fatalf("Vite reloads the page instead of updating %s: %s", path, msg)
		case "update":
			for _, u := range payload.Updates {
				if u.Path == path {
					t.Logf("HMR: %s", msg)
					return
				}
			}
		}
	}
}

// tifa dev exits when its watcher fails, also when Vite's dev server reports a change outside the
// app while the app shuts down: nothing waits for that notice any more.
func TestDevExitsWhileOutsideFilesChange(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a file without permissions, so the watcher would not fail")
	}
	dir := devApp(t)
	card := outsideCard(t, dir, "")
	write := writeFunc(t)
	route := filepath.Join(dir, "app", "route.tsx")
	// The app takes its time to shut down, so the change outside arrives while it does.
	started := filepath.Join(dir, "shutdown.started")
	write(filepath.Join(dir, "hooks.go"), fmt.Sprintf(`package pagestest

import (
	"context"
	"os"
	"time"
)

func OnShutdown(context.Context) error {
	os.WriteFile(%q, nil, 0o644)
	time.Sleep(2 * time.Second)
	return nil
}
`, started))
	d := startDev(t, dir)
	d.waitLog(t, 0, ready)
	expectBody(t, d.base+"/", "outside-one")
	// Loading the module makes Vite's dev server watch it.
	expectBody(t, d.base+"/__tifa/vite/@fs"+filepath.ToSlash(card), "outside-one")

	d.stopped = true
	if err := os.Chmod(route, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(route, 0o644)
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the watcher error did not stop the app\n%s", d.log)
		}
		time.Sleep(20 * time.Millisecond)
	}
	write(card, "export const card = 'outside-two'\n")
	select {
	case err := <-d.exited:
		if err == nil || !strings.Contains(d.log.String(), "permission denied") {
			t.Fatalf("tifa dev exited with %v, want the watcher's error\n%s", err, d.log)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("tifa dev did not exit after its watcher failed\n%s", d.log)
	}
}

// writeFunc returns a function that writes a file or fails the test.
func writeFunc(t *testing.T) func(name, content string) {
	return func(name, content string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// outsideCard makes the home page of the app in dir import card.ts from a directory outside the
// app, and returns the path of card.ts. plugins go into the Vite config after tifa and react.
func outsideCard(t *testing.T, dir, plugins string) string {
	t.Helper()
	shared, err := filepath.EvalSymlinks(t.TempDir()) // Vite allows files by their real path
	if err != nil {
		t.Fatal(err)
	}
	write := writeFunc(t)
	card := filepath.Join(shared, "card.ts")
	write(card, "export const card = 'outside-one'\n")
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Join(realDir, "app"), filepath.Join(shared, "card"))
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(dir, "vite.config.ts"), fmt.Sprintf(`import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tifa from './.tifa/vite.js'

export default defineConfig({ plugins: [tifa(), react(), %s], server: { fs: { allow: ['.', %q] } } })
`, plugins, shared))
	write(filepath.Join(dir, "app", "route.tsx"), fmt.Sprintf("import { card } from %q\nexport default function Home() { return <main>{card}</main> }\n", filepath.ToSlash(rel)))
	return card
}

// waitBody waits until a GET of url answers 200 with want in the body.
func waitBody(t *testing.T, url, want string, d *devRun) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		status, _, body := get(t, url)
		if status == 200 && strings.Contains(body, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s: %d, want 200 with %q:\n%.400s\n%s", url, status, want, body, d.log)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Vite's dev server stops watching while it restarts, here to poll once its watcher ran out of
// watches. A file outside the app that changes meanwhile is in the next server render all the
// same: tifa dev builds the SSR bundle again after each restart.
func TestDevRebuildsAfterViteRestarts(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("tifa dev needs interrupts, which Windows lacks")
	}
	dir := devApp(t)
	closing := filepath.Join(dir, "closing")
	card := outsideCard(t, dir, fmt.Sprintf(`{ name: 'control', apply: 'serve',
  configureServer(server) {
    server.middlewares.use('/__tifa/vite/__test/enospc', (req, res) => {
      res.end('ok')
      server.watcher.emit('error', Object.assign(new Error('ENOSPC'), { code: 'ENOSPC' }))
    })
  },
  async closeBundle() {
    (await import('node:fs')).writeFileSync(%q, '')
    await new Promise((resolve) => setTimeout(resolve, 1500))
  } }`, closing))
	d := startDev(t, dir)
	d.waitLog(t, 0, ready)
	expectBody(t, d.base+"/", "outside-one")
	expectBody(t, d.base+"/__tifa/vite/@fs"+filepath.ToSlash(card), "outside-one")

	expectBody(t, d.base+"/__tifa/vite/__test/enospc", "ok")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(closing); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Vite's dev server did not restart\n%s", d.log)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(250 * time.Millisecond) // the old watcher is closed, the new one does not know the card
	writeFunc(t)(card, "export const card = 'outside-two'\n")
	waitBody(t, d.base+"/", "outside-two", d)
}
