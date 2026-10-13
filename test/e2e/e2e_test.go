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
	exe = filepath.Join(t.TempDir(), app)
	cmd := exec.Command(cli(t), "build", "--dir", dir, "-o", exe)
	// The build needs no Node: run it with a PATH that has none.
	cmd.Env = append(os.Environ(), "PATH="+withoutNode(t))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tifa build %s: %v\n%s", app, err, out)
	}
	info, err := os.Stat(exe)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("tifa build did not produce %s: %v", exe, err)
	}
	return dir, exe
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

// lockedBuffer collects the output of a running process.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
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

var listening = regexp.MustCompile(`listening on (http://\S+)`)

// start runs exe on a free port and returns its base URL once it listens.
func start(t *testing.T, exe string) (base string, cmd *exec.Cmd, output *lockedBuffer) {
	t.Helper()
	output = &lockedBuffer{}
	cmd = exec.Command(exe, "--server-addr", "127.0.0.1:0")
	// The app rejects unknown TIFA_* variables as settings, TIFA_E2E among them.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TIFA_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = output
	if err := cmd.Start(); err != nil {
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
		scanner := bufio.NewScanner(io.TeeReader(stderr, output))
		for scanner.Scan() {
			if m := listening.FindStringSubmatch(scanner.Text()); m != nil {
				found <- m[1]
			}
		}
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
	dir, exe := build(t, "api")
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
	} {
		status, header, body := request(t, tc.method, base+tc.path, tc.body)
		if status != tc.status || body != tc.want {
			t.Errorf("%s %s: %d %s, want %d %s", tc.method, tc.path, status, body, tc.status, tc.want)
		}
		if tc.path == "/orders/7" && header.Get("X-Pattern") != "/orders/{orderId}" {
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
	if !strings.Contains(output.String(), "apitest: shut down") {
		t.Errorf("OnShutdown did not run:\n%s", output)
	}
}

func TestPagesApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir, exe := build(t, "pages")
	for _, name := range []string{"routes.tsx", "server.ts", "client.ts", "tsconfig.json", "app/shop/+route.ts", "runtime/client.ts"} {
		if _, err := os.Stat(filepath.Join(dir, ".tifa", filepath.FromSlash(name))); err != nil {
			t.Errorf("generated %s: %v", name, err)
		}
	}

	base, _, _ := start(t, exe)
	var built struct{ BuildID, Script string }
	readJSON(t, filepath.Join(dir, ".tifa", "dist", "build.json"), &built)

	// A page renders in the VM with the Loads' data.
	status, header, body := get(t, base+"/shops/north/items/apple")
	if status != 200 || header.Get("X-Tifa-Render") != "vm" || header.Get("Vary") != "X-Tifa-Route" {
		t.Errorf("page: %d %v", status, header)
	}
	for _, want := range []string{"<html", "Apple", "Shop north", `src="` + built.Script + `"`, `{id:"north",name:"Shop north"}`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s:\n%s", want, body)
		}
	}
	// The same data gives the same ETag, answered 304.
	if status, _, _ := get(t, base+"/shops/north/items/apple", "If-None-Match", header.Get("ETag")); status != 304 {
		t.Errorf("If-None-Match: %d", status)
	}
	// SSR = false skips the VM and carries the data.
	status, header, body = get(t, base+"/login")
	if status != 200 || header.Get("X-Tifa-Render") != "none" || !strings.Contains(body, `id="tifa-data"`) {
		t.Errorf("SSR = false: %d %v\n%s", status, header, body)
	}
	// Cache = true renders once.
	_, first, _ := get(t, base+"/shops/south")
	_, second, _ := get(t, base+"/shops/south")
	if first.Get("X-Tifa-Render") != "vm" || second.Get("X-Tifa-Render") != "cache" {
		t.Errorf("Cache = true: %s then %s", first.Get("X-Tifa-Render"), second.Get("X-Tifa-Render"))
	}
	// A navigation asks for the listed routes and gets their results by route ID.
	status, header, body = get(t, base+"/shops/north/items/apple", "X-Tifa-Route", "app/shop,app/shop/items", "X-Tifa-Build", built.BuildID)
	if status != 200 || header.Get("Vary") != "X-Tifa-Route" || body != `{"app/shop":{"status":200,"data":{"id":"north","name":"Shop north"}},"app/shop/items":{"status":200,"data":{"id":"apple","title":"Apple","price":1.5,"tags":["fruit"]}}}` {
		t.Errorf("data request: %d %v %s", status, header, body)
	}
	if status, _, _ := get(t, base+"/shops/north", "X-Tifa-Route", "app/shop", "X-Tifa-Build", "old"); status != 409 {
		t.Errorf("other build: %d", status)
	}
	// 404 at every level renders the right NotFound with status 404.
	for path, want := range map[string]string{
		"/nowhere":                "This page does not exist.",
		"/shops/north/items/pear": "No such item.",
		"/shops/closed":           "This page does not exist.",
	} {
		status, _, body := get(t, base+path)
		if status != 404 || !strings.Contains(body, want) || !strings.Contains(body, `src="`+built.Script+`"`) {
			t.Errorf("%s: %d\n%s", path, status, body)
		}
	}
	// Bundle files are served with a long cache.
	if status, header, _ := get(t, base+built.Script); status != 200 || !strings.Contains(header.Get("Cache-Control"), "immutable") {
		t.Errorf("%s: %d %v", built.Script, status, header)
	}

}
