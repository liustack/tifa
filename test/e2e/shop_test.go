package e2e

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shopDir is the example application. Its node_modules must be installed
// (pnpm install --frozen-lockfile in examples/shop).
func shopDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "examples", "shop")
}

// sidebar is in the root Layout of the shop, so its presence tells which level a 404 rendered at.
const sidebar = `aria-label="工作区导航"`

func TestShopApp(t *testing.T) {
	skipUnlessEnabled(t)
	dir := shopDir(t)
	exe := buildAt(t, dir)

	// go vet and go test pass in the app and do not reach into .tifa.
	for _, args := range [][]string{{"list", "./..."}, {"vet", "./..."}, {"test", "-count=1", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil || strings.Contains(string(out), ".tifa") {
			t.Errorf("go %s in the shop: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	base, _, _ := start(t, exe)
	var built struct{ BuildID string }
	readJSON(t, filepath.Join(dir, ".tifa", "dist", "build.json"), &built)

	// The site root sends visitors to the first project.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 302 || res.Header.Get("Location") != "/projects/studio" {
		t.Errorf("GET /: %d %s", res.StatusCode, res.Header.Get("Location"))
	}

	// The order page is in the HTML, rendered in the VM with both Layouts around it.
	detail := base + "/projects/studio/orders/SH-1042"
	status, header, body := get(t, detail)
	if status != 200 || header.Get("X-Tifa-Render") != "vm" {
		t.Errorf("order page: %d %v", status, header)
	}
	for _, want := range []string{"<title>SH-1042 · 山海工作台</title>", "林夏", "晨间手冲壶", "¥428.00", "山海工作室", "2 单待发货", sidebar, "正在准备操作"} {
		if !strings.Contains(body, want) {
			t.Errorf("order page lacks %s", want)
		}
	}
	// Same data, same ETag: 304. The order page sets Cache = true: the next request skips the VM.
	if status, _, _ := get(t, detail, "If-None-Match", header.Get("ETag")); status != 304 {
		t.Errorf("If-None-Match: %d", status)
	}
	if _, again, _ := get(t, detail); again.Get("X-Tifa-Render") != "cache" {
		t.Errorf("second request of a Cache = true page: %s", again.Get("X-Tifa-Render"))
	}

	// 404 at each level: the order inside the project Layout, the project inside the root Layout,
	// an unknown URL on its own.
	for _, tc := range []struct {
		path, want  string
		withSidebar bool
	}{
		{"/projects/studio/orders/WK-0201", "没有找到这个订单</h3>", true},
		{"/projects/nope", "没有找到这个页面", true},
		{"/nowhere", "没有找到这个页面", false},
	} {
		status, _, body := get(t, base+tc.path)
		// The project Layout shows the project name in its heading.
		project := strings.Contains(body, "<h1>山海工作室</h1>")
		if status != 404 || !strings.Contains(body, tc.want) || strings.Contains(body, sidebar) != tc.withSidebar || project != (tc.path == "/projects/studio/orders/WK-0201") {
			t.Errorf("%s: %d, want 404 with %q, sidebar %v\n%s", tc.path, status, tc.want, tc.withSidebar, body)
		}
	}

	// Shipping: only from a page of this host, once.
	if status := post(t, base+"/projects/studio/orders/SH-1042", ""); status != 403 {
		t.Errorf("POST without Origin: %d", status)
	}
	if status := post(t, base+"/projects/studio/orders/SH-1042", "http://other.example"); status != 403 {
		t.Errorf("POST from another site: %d", status)
	}
	if status := post(t, base+"/projects/studio/orders/SH-1042", base); status != 204 {
		t.Errorf("ship: %d", status)
	}
	if status := post(t, base+"/projects/studio/orders/SH-1042", base); status != 409 {
		t.Errorf("ship twice: %d", status)
	}
	// The navigation data of the list now carries the new totals, in one response for all routes.
	status, _, body = get(t, base+"/projects/studio", "X-Tifa-Route", "app,app/projects,app/projects/orders/list", "X-Tifa-Build", built.BuildID)
	for _, want := range []string{`"app/projects":{"status":200,"data":{"id":"studio","name":"山海工作室"`, `"pending":1`, `"shipped":3`, `"app":{"status":200`} {
		if status != 200 || !strings.Contains(body, want) {
			t.Errorf("list data after shipping: %d lacks %s\n%s", status, want, body)
		}
	}
	if status, _, _ := get(t, base+"/projects/studio", "X-Tifa-Route", "app/projects", "X-Tifa-Build", "old"); status != 409 {
		t.Errorf("data request of another build: %d", status)
	}
}

func post(t *testing.T, url, origin string) int {
	t.Helper()
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	res.Body.Close()
	return res.StatusCode
}

// Static analysis does not run user code: a package of the shop whose init() writes a file
// builds, and the file appears only when the built program runs.
func TestShopBuildDoesNotRunInit(t *testing.T) {
	skipUnlessEnabled(t)
	app := copyShop(t)
	marker := filepath.Join(t.TempDir(), "init-ran")
	write(t, filepath.Join(app, "internal", "sideeffect", "sideeffect.go"), `package sideeffect

import "os"

func init() { os.WriteFile(`+"`"+marker+"`"+`, []byte("init ran"), 0o644) }
`)
	route := filepath.Join(app, "app", "projects", "orders", "list", "route.go")
	replaceIn(t, route, "import (\n", "import (\n\t_ \"github.com/liustack/tifa/examples/shop/internal/sideeffect\"\n")

	exe := buildAt(t, app)
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("tifa build ran init(): stat %s: %v", marker, err)
	}
	// The package is part of the program: running it runs init().
	if out, err := exec.Command(exe, "version").CombinedOutput(); err != nil {
		t.Fatalf("%s version: %v\n%s", exe, err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the built program did not run init(): %v", err)
	}
}

// Every convention violation fails tifa build with the file and line, using the analyzer's fixtures.
func TestBuildReportsViolations(t *testing.T) {
	skipUnlessEnabled(t)
	for _, tc := range []struct{ fixture, want string }{
		{"duppath", "app/b/route.go:3:"},
		{"ongetpage", "app/page/route.go:7:"},
		{"layoutparam", "app/projects/layout.go:6:"},
		{"usenonancestor", "app/shop/route.go:11:"},
		{"badtype", "app/page/route.go:56:"},
	} {
		dir := filepath.Join(repoRoot(t), "internal", "analyze", "testdata", tc.fixture)
		cmd := exec.Command(cli(t), "build", "--dir", dir, "-o", filepath.Join(t.TempDir(), "app"))
		out, err := cmd.CombinedOutput()
		// Each violation is a line that starts with the file and line, relative to the app.
		if err == nil || !strings.HasPrefix(string(out), tc.want) && !strings.Contains(string(out), "\n"+tc.want) {
			t.Errorf("%s: %v, want a failure naming %s\n%s", tc.fixture, err, tc.want, out)
		}
		if _, err := os.Stat(filepath.Join(dir, ".tifa")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: a failed analysis generated .tifa: %v", tc.fixture, err)
		}
	}
}

// copyShop copies the shop's sources into a temporary module that points at this repository
// and shares the shop's node_modules.
func copyShop(t *testing.T) string {
	t.Helper()
	src, dst := shopDir(t), filepath.Join(t.TempDir(), "shop")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch rel {
			case "node_modules", ".tifa", "dist":
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(src, "node_modules"), filepath.Join(dst, "node_modules")); err != nil {
		t.Fatal(err)
	}
	replaceIn(t, filepath.Join(dst, "go.mod"), "=> ../..", "=> "+repoRoot(t))
	return dst
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceIn(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s lacks %q", path, old)
	}
	write(t, path, strings.Replace(string(data), old, new, 1))
}
