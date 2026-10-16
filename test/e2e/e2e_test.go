// Package e2e builds the tifa CLI, runs tifa build on the applications in test/apps and checks
// the results from the outside: the executable, its HTTP behaviour, its hooks and the type check
// of the generated TypeScript. The tests are slow and skipped unless TIFA_E2E=1.
package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var (
	cliOnce sync.Once
	cliPath string
	cliErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if cliPath != "" {
		os.RemoveAll(filepath.Dir(cliPath))
	}
	os.Exit(code)
}

func skipUnlessEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("TIFA_E2E") != "1" {
		t.Skip("end-to-end test, set TIFA_E2E=1 to run it")
	}
}

// repoRoot is the framework repository, two levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// cli builds the tifa command once for all tests.
func cli(t *testing.T) string {
	t.Helper()
	cliOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tifa-e2e-")
		if err != nil {
			cliErr = err
			return
		}
		cliPath = filepath.Join(dir, "tifa")
		cmd := exec.Command("go", "build", "-o", cliPath, "./cmd/tifa")
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			cliErr = &buildError{err: err, out: out}
		}
	})
	if cliErr != nil {
		t.Fatalf("build tifa: %v", cliErr)
	}
	return cliPath
}

type buildError struct {
	err error
	out []byte
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + string(e.out) }

// build runs tifa build on test/apps/<app> and returns the executable.
func build(t *testing.T, app string) (dir, exe string) {
	t.Helper()
	dir = filepath.Join(repoRoot(t), "test", "apps", app)
	return dir, buildAt(t, dir)
}

// buildAt runs tifa build on the application in dir, whose Vite runs on Node, and returns the
// executable.
func buildAt(t *testing.T, dir string) (exe string) {
	t.Helper()
	return buildWith(t, dir, os.Getenv("PATH"))
}

// buildWith runs tifa build on the application in dir with the given PATH and returns the
// executable.
func buildWith(t *testing.T, dir, path string) (exe string) {
	t.Helper()
	app := filepath.Base(dir)
	exe = filepath.Join(t.TempDir(), app)
	// go mod tidy keeps every module the generated main needs, because the app lists
	// cmd/tifa as a tool, so a tidy go.mod builds with -mod=readonly and stays unchanged.
	tidy := exec.Command("go", "mod", "tidy", "-diff")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy would change %s: %v\n%s", app, err, out)
	}
	before := moduleFiles(t, dir)
	cmd := exec.Command(cli(t), "build", "--dir", dir, "-o", exe)
	cmd.Env = append(os.Environ(), "PATH="+path, "GOFLAGS=-mod=readonly")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tifa build %s: %v\n%s", app, err, out)
	}
	if after := moduleFiles(t, dir); after != before {
		t.Errorf("tifa build changed go.mod or go.sum of %s", app)
	}
	info, err := os.Stat(exe)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("tifa build did not produce %s: %v", exe, err)
	}
	return exe
}

// typecheck runs tsc --noEmit in the app at dir, so the generated TypeScript in .tifa and the
// app's TSX check together. It skips when the app's npm packages or node are not installed.
func typecheck(t *testing.T, dir string) {
	t.Run("tsc", func(t *testing.T) {
		tsc := filepath.Join(dir, "node_modules", ".bin", "tsc")
		if _, err := os.Stat(tsc); err != nil {
			t.Skipf("run pnpm install --frozen-lockfile in %s for the type check: %v", dir, err)
		}
		if _, err := exec.LookPath("node"); err != nil {
			t.Skip("node is not installed, skipping the type check")
		}
		cmd := exec.Command(tsc, "--noEmit")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc --noEmit: %v\n%s", err, out)
		}
	})
}

// moduleFiles returns go.mod and go.sum of the app in dir.
func moduleFiles(t *testing.T, dir string) string {
	t.Helper()
	var all string
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		all += string(data)
	}
	return all
}

// withoutNode returns PATH without the directories that hold a node executable.
func withoutNode(t *testing.T) string {
	t.Helper()
	var dirs []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(dir, "node")); err != nil {
			dirs = append(dirs, dir)
		}
	}
	path := strings.Join(dirs, string(filepath.ListSeparator))
	if node, err := lookPath("node", path); err == nil {
		t.Fatalf("PATH still finds %s", node)
	}
	return path
}

// lookPath is exec.LookPath in the directories of path.
func lookPath(file, path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		candidate := filepath.Join(dir, file)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

// lockedBuffer collects the output of a running process. A test reads it while the process
// runs, so a line the process wrote before answering a request may not be here yet: wait for it
// with waitFor, or for the end of the output with drained.
type lockedBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	drained chan struct{} // closed once the process closed its output and all of it is here
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor waits until ok holds for the output, and fails the test after ten seconds.
func (b *lockedBuffer) waitFor(t *testing.T, ok func(output string) bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(b.String()); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("output never got there:\n%s", b)
		}
	}
}

var listening = regexp.MustCompile(`listening on (http://\S+)`)

// start runs exe on a free port and returns its base URL once it listens. Its PATH has no node:
// a built application never needs it.
func start(t *testing.T, exe string) (base string, cmd *exec.Cmd, output *lockedBuffer) {
	t.Helper()
	output = &lockedBuffer{drained: make(chan struct{})}
	cmd = exec.Command(exe, "--server-addr", "127.0.0.1:0")
	// The app rejects unknown TIFA_* variables as settings, TIFA_E2E among them.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TIFA_") && !strings.HasPrefix(kv, "PATH=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+withoutNode(t))
	// Stdout and stderr share one pipe, so the output keeps the order the process wrote it in:
	// a line printed by OnStart comes before the line saying the app listens.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = w, w
	err = cmd.Start()
	w.Close()
	if err != nil {
		r.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	found := make(chan string, 1)
	go func() {
		defer close(output.drained)
		defer r.Close()
		scanner := bufio.NewScanner(io.TeeReader(r, output))
		for scanner.Scan() {
			if m := listening.FindStringSubmatch(scanner.Text()); m != nil {
				found <- m[1]
			}
		}
		io.Copy(output, r) // a line too long for the scanner
	}()
	select {
	case base = <-found:
	case <-time.After(20 * time.Second):
		t.Fatalf("%s did not start listening:\n%s", exe, output)
	}
	return base, cmd, output
}

func get(t *testing.T, url string, header ...string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header, string(data)
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

func request(t *testing.T, method, url, body string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header, string(data)
}

func TestInterfaceApp(t *testing.T) {
	skipUnlessEnabled(t)
	if runtime.GOOS == "windows" {
		t.Skip("the test stops the app with SIGINT")
	}
	// An application without pages builds without Node.
	dir := filepath.Join(repoRoot(t), "test", "apps", "api")
	exe := buildWith(t, dir, withoutNode(t))
	if _, err := os.Stat(filepath.Join(dir, ".tifa", "routes.tsx")); !os.IsNotExist(err) {
		t.Errorf("an app without pages got TypeScript: %v", err)
	}
	list := exec.Command("go", "list", "./...")
	list.Dir = dir
	if out, err := list.CombinedOutput(); err != nil || strings.Contains(string(out), ".tifa") {
		t.Errorf("go list ./... in the app: %v\n%s", err, out)
	}

	base, cmd, output := start(t, exe)
	if !strings.Contains(output.String(), "apitest: started") {
		t.Errorf("OnStart did not run before listening:\n%s", output)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
		want               string
	}{
		{"GET", "/", "", 200, "home"},
		{"GET", "/health", "", 200, `{"status":"ok"}`},
		{"GET", "/orders/7", "", 200, `{"id":"7","note":""}`},
		{"POST", "/orders/9", `{"note":"gift"}`, 201, `{"id":"9","note":"gift"}`},
		{"GET", "/orders/missing", "", 404, `{"error":"Not Found"}`},
		{"GET", "/nowhere", "", 404, `{"error":"Not Found"}`},
		{"GET", "/orders/shipped", "", 409, `{"error":"order already shipped"}`},
		{"GET", "/orders/broken", "", 503, `{"error":"try again later"}`},
		{"DELETE", "/orders/7", "", 405, `{"error":"Method Not Allowed"}`},
		{"GET", "/notes?tag=x&limit=1", "", 200, `[{"text":"hi","tag":"x"}]`},
		{"GET", "/notes?limit=many", "", 400, `{"error":"invalid query parameter \"limit\""}`},
		{"POST", "/notes", `{"text":"a","tag":"t"}`, 200, `{"text":"a","tag":"t"}`},
		{"POST", "/notes", `{"text":""}`, 422, `{"error":"Unprocessable Entity","fields":{"text":"write something"}}`},
		{"POST", "/notes", `{"text":"far more than twenty letters"}`, 400, `{"error":"text is too long"}`},
		{"DELETE", "/notes?tag=x", "", 204, ""},
	} {
		status, header, body := request(t, tc.method, base+tc.path, tc.body)
		if status != tc.status || body != tc.want {
			t.Errorf("%s %s: %d %s, want %d %s", tc.method, tc.path, status, body, tc.status, tc.want)
		}
		if tc.path == "/orders/7" && header.Get("X-Pattern") != "/orders/{order}" {
			t.Errorf("OnRequest saw pattern %q", header.Get("X-Pattern"))
		}
	}

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("app exited with %v:\n%s", err, output)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("app did not stop after SIGINT:\n%s", output)
	}
	<-output.drained
	if !strings.Contains(output.String(), "apitest: shut down") {
		t.Errorf("OnShutdown did not run:\n%s", output)
	}
}

var (
	headTags = regexp.MustCompile(`^<link rel="stylesheet" href="/assets/style-[\w-]+\.css">(<link rel="modulepreload" href="/assets/chunk-[\w-]+\.js">)*<script type="module" src="/assets/app-[\w-]+\.js"></script>$`)
	headURL  = regexp.MustCompile(`(?:href|src)="([^"]+)"`)
)

func TestPagesApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir, exe := build(t, "pages")
	for _, name := range []string{"routes.tsx", "server.ts", "client.ts", "tsconfig.json", "app/shops/shop+/+layout.ts", "runtime/client.ts"} {
		if _, err := os.Stat(filepath.Join(dir, ".tifa", filepath.FromSlash(name))); err != nil {
			t.Errorf("generated %s: %v", name, err)
		}
	}

	typecheck(t, dir)

	base, _, _ := start(t, exe)
	var built struct{ BuildID, Head string }
	readJSON(t, filepath.Join(dir, ".tifa", "dist", "build.json"), &built)
	// The head loads the stylesheet, the shared chunks and the entry, in that order.
	if !headTags.MatchString(built.Head) {
		t.Fatalf("build.json head %s", built.Head)
	}

	// A page renders in the VM with the Loads' data.
	status, header, body := get(t, base+"/shops/north/items/apple")
	if status != 200 || header.Get("X-Tifa-Render") != "vm" || header.Get("Vary") != "X-Tifa-Route" {
		t.Errorf("page: %d %v", status, header)
	}
	for _, want := range []string{"<html", "Apple", "Shop north", built.Head, `{id:"north",name:"Shop north"}`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s:\n%s", want, body)
		}
	}
	// Intl formats in the locale and the time zone the page passes, as Chromium does.
	_, _, body = get(t, base+"/receipt")
	for _, want := range []string{"¥1,234.50", "2025/8/17", "2025年8月17日星期日 04:05"} {
		if !strings.Contains(body, want) {
			t.Errorf("receipt lacks %s:\n%s", want, body)
		}
	}
	// The same data gives the same ETag, answered 304.
	if status, _, _ := get(t, base+"/shops/north/items/apple", "If-None-Match", header.Get("ETag")); status != 304 {
		t.Errorf("If-None-Match: %d", status)
	}
	// SSR = false skips the VM and carries the data.
	status, header, body = get(t, base+"/account/login")
	if status != 200 || header.Get("X-Tifa-Render") != "none" || !strings.Contains(body, `id="tifa-data"`) || !strings.Contains(body, built.Head) {
		t.Errorf("SSR = false: %d %v\n%s", status, header, body)
	}
	// Cache = true renders once.
	_, first, _ := get(t, base+"/shops/south")
	_, second, _ := get(t, base+"/shops/south")
	if first.Get("X-Tifa-Render") != "vm" || second.Get("X-Tifa-Render") != "cache" {
		t.Errorf("Cache = true: %s then %s", first.Get("X-Tifa-Render"), second.Get("X-Tifa-Render"))
	}
	// A navigation asks for the listed routes and gets their results by route ID.
	status, header, body = get(t, base+"/shops/north/items/apple", "X-Tifa-Route", "app/shops/shop+/layout,app/shops/shop+/items/item+/route", "X-Tifa-Build", built.BuildID)
	if status != 200 || header.Get("Vary") != "X-Tifa-Route" || body != `{"app/shops/shop+/items/item+/route":{"status":200,"data":{"id":"apple","title":"Apple","price":1.5,"tags":["fruit"]}},"app/shops/shop+/layout":{"status":200,"data":{"id":"north","name":"Shop north"}}}` {
		t.Errorf("data request: %d %v %s", status, header, body)
	}
	if status, _, _ := get(t, base+"/shops/north", "X-Tifa-Route", "app/shops/shop+/layout", "X-Tifa-Build", "old"); status != 409 {
		t.Errorf("other build: %d", status)
	}
	// 404 at every level renders the right NotFound with status 404.
	for path, want := range map[string]string{
		"/nowhere":                "This page does not exist.",
		"/shops/north/items/pear": "No such item.",
		"/shops/closed":           "This page does not exist.",
	} {
		status, _, body := get(t, base+path)
		if status != 404 || !strings.Contains(body, want) || !strings.Contains(body, built.Head) {
			t.Errorf("%s: %d\n%s", path, status, body)
		}
	}
	// Bundle files are served with a long cache and their type.
	for _, m := range headURL.FindAllStringSubmatch(built.Head, -1) {
		status, header, _ := get(t, base+m[1])
		kind := "text/javascript"
		if strings.HasSuffix(m[1], ".css") {
			kind = "text/css"
		}
		if status != 200 || !strings.Contains(header.Get("Cache-Control"), "immutable") || !strings.HasPrefix(header.Get("Content-Type"), kind) {
			t.Errorf("%s: %d %v", m[1], status, header)
		}
	}

}
