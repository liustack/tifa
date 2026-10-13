// Package e2e builds the tifa CLI, runs tifa build on the applications in test/apps and checks
// the results from the outside: the executable, its HTTP behaviour, its hooks and the type check
// of the generated TypeScript. The tests are slow and skipped unless TIFA_E2E=1.
package e2e

import (
	"bufio"
	"bytes"
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
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tifa build %s: %v\n%s", app, err, out)
	}
	info, err := os.Stat(exe)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("tifa build did not produce %s: %v", exe, err)
	}
	return dir, exe
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

	// Without a renderer a page answers its Load chain as JSON.
	base, _, _ := start(t, exe)
	status, _, body := request(t, "GET", base+"/shops/north/items/apple", "")
	if status != 200 || !strings.Contains(body, `"app/shop":{"id":"north","name":"Shop north"}`) {
		t.Errorf("page GET: %d %s", status, body)
	}
	if status, _, _ := request(t, "GET", base+"/shops/closed", ""); status != 404 {
		t.Errorf("Layout ErrNotFound: %d", status)
	}

	pnpm, err := exec.LookPath("pnpm")
	if err != nil {
		t.Skip("pnpm is not installed, skipping the type check")
	}
	install := exec.Command(pnpm, "install", "--frozen-lockfile", "--prefer-offline")
	install.Dir = dir
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("pnpm install: %v\n%s", err, out)
	}
	tsc := exec.Command(pnpm, "exec", "tsc", "--noEmit")
	tsc.Dir = dir
	if out, err := tsc.CombinedOutput(); err != nil {
		t.Fatalf("tsc --noEmit: %v\n%s", err, out)
	}
}
