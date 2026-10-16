package e2e

import (
	"errors"
	"io"
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
	typecheck(t, dir)

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
	var built struct{ BuildID, Head string }
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
	for _, want := range []string{"<title>SH-1042 · 山海工作台</title>", "林夏", "晨间手冲壶", "¥428.00", ">山海工作室</h1>", "2</span> 单待发货", sidebar, "确认发货", "选择快递公司"} {
		if !strings.Contains(body, want) {
			t.Errorf("order page lacks %s", want)
		}
	}
	// The list is a table that pages in the browser: the rendered page holds the ten newest
	// orders and the totals, with money and times in Chinese. The data written into the page
	// after it holds all 42.
	_, _, list := get(t, base+"/projects/studio")
	rendered, _, _ := strings.Cut(list[strings.Index(list, `id="tifa-root"`):], "<script")
	for _, want := range []string{">SH-1042</a>", ">SH-1033</a>", "¥14,134.00", "10月13日 14:32"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("order list lacks %s", want)
		}
	}
	if strings.Contains(rendered, "SH-1032") || !strings.Contains(list, `\"id\":\"SH-1001\"`) {
		t.Errorf("the order list renders other than its first page, or lacks the data of the others:\n%s", list)
	}
	// The stats page renders its key numbers. The charts load in the browser only, so the HTML
	// has a placeholder of a fixed height for each, and recharts is in none of the scripts the
	// head loads: the entry and the chunks it preloads.
	status, _, stats := get(t, base+"/projects/studio/stats")
	for _, want := range []string{"<title>销售统计 · 山海工作室 · 山海工作台</title>", "销售额", "¥14,134.00", "95.2%", "热销商品"} {
		if status != 200 || !strings.Contains(stats, want) {
			t.Errorf("stats page: %d lacks %s", status, want)
		}
	}
	if n := strings.Count(stats, `data-slot="skeleton"`); n != 2 {
		t.Errorf("stats page: %d chart placeholders, want 2", n)
	}
	if strings.Contains(stats, "recharts") {
		t.Error("stats page: recharts rendered on the server")
	}
	for _, m := range headURL.FindAllStringSubmatch(built.Head, -1) {
		if _, _, script := get(t, base+m[1]); strings.HasSuffix(m[1], ".js") && strings.Contains(script, "recharts-wrapper") {
			t.Errorf("%s, which every page loads, holds recharts", m[1])
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
	// an unknown URL on its own. Each one keeps a title, the last from the root Layout's documentHead.
	for _, tc := range []struct {
		path, want  string
		withSidebar bool
	}{
		{"/projects/studio/orders/WK-0201", ">没有找到这个订单</div>", true},
		{"/projects/nope", "没有找到这个页面", true},
		{"/projects/nope/stats", "没有找到这个页面", true},
		{"/nowhere", "没有找到这个页面", false},
	} {
		status, _, body := get(t, base+tc.path)
		// The project Layout shows the project name in its heading.
		project := strings.Contains(body, ">山海工作室</h1>")
		if status != 404 || !strings.Contains(body, tc.want) || !strings.Contains(body, "山海工作台</title>") || strings.Contains(body, sidebar) != tc.withSidebar || project != (tc.path == "/projects/studio/orders/WK-0201") {
			t.Errorf("%s: %d, want 404 with %q, sidebar %v\n%s", tc.path, status, tc.want, tc.withSidebar, body)
		}
	}

	// Shipping: only from a page of this host, with a carrier and a tracking number, once.
	order := base + "/projects/studio/orders/SH-1042"
	ship := `{"carrier":"SF","trackingNo":"SF1234567890"}`
	if status, _ := send(t, "POST", order, "", ship); status != 403 {
		t.Errorf("POST without Origin: %d", status)
	}
	if status, _ := send(t, "POST", order, "http://other.example", ship); status != 403 {
		t.Errorf("POST from another site: %d", status)
	}
	if status, body := send(t, "POST", order, base, `{"carrier":"","trackingNo":"SF12"}`); status != 422 || !strings.Contains(body, `"carrier":"请选择快递公司"`) || !strings.Contains(body, `"trackingNo":"运单号是 8 到 20 位字母或数字"`) {
		t.Errorf("ship without a carrier: %d %s", status, body)
	}
	// The recipient can change until the order ships.
	if status, body := send(t, "PATCH", order, base, `{"customer":"林夏","phone":"138 0013 8000","address":"上海市徐汇区武康路 99 号"}`); status != 200 || !strings.Contains(body, `"phone":"13800138000"`) {
		t.Errorf("change the recipient: %d %s", status, body)
	}
	if status, body := send(t, "POST", order, base, ship); status != 200 || !strings.Contains(body, `"carrierName":"顺丰速运"`) {
		t.Errorf("ship: %d %s", status, body)
	}
	if status, _ := send(t, "POST", order, base, ship); status != 409 {
		t.Errorf("ship twice: %d", status)
	}
	if status, _ := send(t, "PATCH", order, base, `{"customer":"林夏","phone":"13800138000","address":"上海市"}`); status != 409 {
		t.Errorf("change the recipient of a shipped order: %d", status)
	}
	// The navigation data of the list now carries the new totals, in one response for all routes.
	status, _, body = get(t, base+"/projects/studio", "X-Tifa-Route", "app/layout,app/projects/project+/layout,app/projects/project+/route", "X-Tifa-Build", built.BuildID)
	for _, want := range []string{`"app/projects/project+/layout":{"status":200,"data":{"id":"studio","name":"山海工作室"`, `"pending":1`, `"shipped":41`, `"app/layout":{"status":200`} {
		if status != 200 || !strings.Contains(body, want) {
			t.Errorf("list data after shipping: %d lacks %s\n%s", status, want, body)
		}
	}
	if status, _, _ := get(t, base+"/projects/studio", "X-Tifa-Route", "app/projects/project+/layout", "X-Tifa-Build", "old"); status != 409 {
		t.Errorf("data request of another build: %d", status)
	}
}

// send makes a request with a JSON body from origin, or with no Origin when it is empty, and
// returns the status and the body of the answer.
func send(t *testing.T, method, url, origin, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return res.StatusCode, string(b)
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
	route := filepath.Join(app, "app", "projects", "project+", "route.go")
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
		{"duppath", "app/items/key+/route.go:1:"},
		{"ongetpage", "app/page/route.go:7:"},
		{"layoutparam", "app/projects/layout.go:6:"},
		{"usenonancestor", "app/shop/route.go:11:"},
		{"badtype", "app/page/route.go:56:"},
		{"bindquery", "app/list/route.go:12:"},
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
