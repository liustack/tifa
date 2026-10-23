// Package ssr_test runs the SSR pool of internal/ssr on the real React bundle of the example app
// in examples/shop, which it treats as an app from outside: it reads the build output and asks the
// built program for the data of each page. The tests skip unless TIFA_SSR_SHOP=1. Build the shop
// first, then run:
//
//	cd examples/shop && pnpm install --frozen-lockfile && go tool tifa build
//	TIFA_SSR_SHOP=1 go test -run Shop -v ./test/ssr
package ssr_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/slowjs"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

const shopDir = "../../examples/shop"

type shopPage struct {
	input           ssr.Input
	want, forbidden string
}

// shopRoutes are the comparison pages: the four of the prototype baseline, a list and a detail
// page of each project, then the stats page of each project. Each comes with the route IDs of its
// match chain, a text its HTML must have and one of the other project that it must not have.
var shopRoutes = []struct{ url, routes, want, forbidden string }{
	{"/projects/studio", "app/layout,app/projects/project+/layout,app/projects/project+/route", "SH-1042", "WK-0201"},
	{"/projects/weekend", "app/layout,app/projects/project+/layout,app/projects/project+/route", "WK-0201", "SH-1042"},
	{"/projects/studio/orders/SH-1042", "app/layout,app/projects/project+/layout,app/projects/project+/orders/order+/route", "SH-1042", "WK-0201"},
	{"/projects/weekend/orders/WK-0201", "app/layout,app/projects/project+/layout,app/projects/project+/orders/order+/route", "WK-0201", "SH-1042"},
	{"/projects/studio/stats", "app/layout,app/projects/project+/layout,app/projects/project+/stats/route", "¥14,134.00", "¥1,689.00"},
	{"/projects/weekend/stats", "app/layout,app/projects/project+/layout,app/projects/project+/stats/route", "¥1,689.00", "¥14,134.00"},
}

// shopBundle returns the shop's SSR bundle and its comparison pages. The data of each
// page is what the shop's own program injects: the test runs bin/shop and asks it for the
// navigation data of each page, and writes the data of each route as JSON text, as the program
// injects it (see injectedData).
func shopBundle(t testing.TB) (string, []shopPage) {
	t.Helper()
	if os.Getenv("TIFA_SSR_SHOP") == "" {
		t.Skip("set TIFA_SSR_SHOP=1 after running tifa build in examples/shop")
	}
	source, err := os.ReadFile(filepath.Join(shopDir, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatalf("run tifa build in examples/shop first: %v", err)
	}
	var built struct{ BuildID string }
	data, err := os.ReadFile(filepath.Join(shopDir, ".tifa", "dist", "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &built); err != nil {
		t.Fatal(err)
	}
	base := startShop(t)
	var pages []shopPage
	for _, r := range shopRoutes {
		req, err := http.NewRequest("GET", base+r.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Tifa-Route", r.routes)
		req.Header.Set("X-Tifa-Build", built.BuildID)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("data of %s: %d %v %s (rebuild the shop when its build.json changed)", r.url, res.StatusCode, err, body)
		}
		pages = append(pages, shopPage{ssr.Input{BuildID: built.BuildID, URL: r.url, Data: injectedData(t, body)}, r.want, r.forbidden})
	}
	return string(source), pages
}

// injectedData turns the answer of a navigation data request into the data a whole-page render
// gets: each successful route carries its data as the JSON text Go wrote, in json, with the
// escapes of ssr.EscapeAboveFFFF the server writes into it.
func injectedData(t testing.TB, answer []byte) json.RawMessage {
	t.Helper()
	var routes map[string]struct {
		Status   int             `json:"status"`
		Data     json.RawMessage `json:"data,omitempty"`
		JSON     string          `json:"json,omitempty"`
		Error    string          `json:"error,omitempty"`
		Location string          `json:"location,omitempty"`
	}
	if err := json.Unmarshal(answer, &routes); err != nil {
		t.Fatal(err)
	}
	for id, r := range routes {
		if r.Status == http.StatusOK {
			r.JSON, r.Data = string(ssr.EscapeAboveFFFF(r.Data)), nil
			if r.JSON == "" { // a nil Data, left out of the answer
				r.JSON = "null"
			}
			routes[id] = r
		}
	}
	data, err := json.Marshal(routes)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// injectedData writes the data of a route as the server does, characters above U+FFFF as the
// escapes of their surrogate pairs among it.
func TestInjectedDataIsWhatTheServerInjects(t *testing.T) {
	data := injectedData(t, []byte(`{"app/route":{"status":200,"data":{"s":"a😀"}},"app/gone":{"status":404}}`))
	var routes map[string]struct{ JSON string }
	if err := json.Unmarshal(data, &routes); err != nil {
		t.Fatal(err)
	}
	if got, want := routes["app/route"].JSON, `{"s":"a\ud83d\ude00"}`; got != want {
		t.Errorf("json = %s, want %s", got, want)
	}
}

var listening = regexp.MustCompile(`listening on (http://\S+)`)

// startShop runs the shop's program built by tifa build and returns its base URL.
func startShop(t testing.TB) string {
	t.Helper()
	cmd := exec.Command(filepath.Join(shopDir, "bin", "shop"), "--server-addr", "127.0.0.1:0")
	// The program rejects unknown TIFA_* variables as settings, TIFA_SSR_SHOP among them.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TIFA_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("run tifa build in examples/shop first: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	found := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if m := listening.FindStringSubmatch(scanner.Text()); m != nil {
				found <- m[1]
			}
		}
	}()
	select {
	case base := <-found:
		return base
	case <-time.After(10 * time.Second):
		t.Fatal("the shop did not start listening")
		return ""
	}
}

func (page shopPage) check(t testing.TB, html string) {
	t.Helper()
	if !page.matches(html) {
		t.Fatalf("wrong HTML for %s", page.input.URL)
	}
}

// matches reports whether html is this page: it has the page's text, not the other project's,
// and the router's data script.
func (page shopPage) matches(html string) bool {
	return strings.Contains(html, page.want) && !strings.Contains(html, page.forbidden) && strings.Contains(html, "data-tsr-stream-part")
}

func TestShopRendersReadyData(t *testing.T) {
	source, pages := shopBundle(t)
	p := newPool(t, source, shopConfig(1))
	detail := pages[2]
	if detail.input.URL != "/projects/studio/orders/SH-1042" {
		t.Fatalf("unexpected page order: %s", detail.input.URL)
	}
	html, err := p.Render(context.Background(), detail.input)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"SH-1042", "林夏", "晨间手冲壶", ">山海工作室</h1>", "2</span> 单待发货", "data-tsr-stream-part", "确认发货"} {
		if !strings.Contains(html, text) {
			t.Errorf("missing %q", text)
		}
	}
	for _, page := range pages {
		html, err := p.Render(context.Background(), page.input)
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
	}
}

func TestShopEscapesScriptClosingTags(t *testing.T) {
	source, pages := shopBundle(t)
	p := newPool(t, source, shopConfig(1))
	in := withCustomer(pages[2].input, `</script><script id="injected">alert(1)</script>`)
	if string(in.Data) == string(pages[2].input.Data) {
		t.Fatal("customer name not found in the snapshot")
	}
	html, err := p.Render(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "alert(1)") {
		t.Fatal("injected customer is missing from the HTML")
	}
	if strings.Contains(html, `<script id="injected">`) {
		t.Fatal("unescaped script in HTML or dehydrated state")
	}
}

// slowestWarmRender is the most a warm render of a shop page may take in the default run. The
// shop's slowest page takes under 200 ms on a laptop, so this catches a render that became many
// times slower, not small changes: measure those with TIFA_SSR_MEASURE=1.
const slowestWarmRender = time.Second

// TestShopWarmLatency renders the comparison pages in one reused VM, timing the whole Render. By
// default it renders each page a few times, checks the HTML and that no warm render is slower than
// slowestWarmRender. With TIFA_SSR_MEASURE=1 it uses the method of the prototype baseline, 512
// warm renders then 2,048 timed ones, for numbers worth comparing.
func TestShopWarmLatency(t *testing.T) {
	source, pages := shopBundle(t)
	warm, timed := 2*len(pages), 8*len(pages)
	if os.Getenv("TIFA_SSR_MEASURE") != "" {
		warm, timed = 512, 2048
	}
	start := time.Now()
	p := newPool(t, source, shopConfig(1))
	t.Logf("startup (compile and load one VM): %s", time.Since(start))
	for i := range warm {
		page := pages[i%len(pages)]
		html, err := p.Render(context.Background(), page.input)
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
	}
	latencies := make([]float64, timed)
	for i := range latencies {
		page := pages[i%len(pages)]
		begin := time.Now()
		html, err := p.Render(context.Background(), page.input)
		took := time.Since(begin)
		latencies[i] = float64(took) / 1e6
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
		if took > slowestWarmRender {
			t.Errorf("warm render of %s took %s, more than %s", page.input.URL, took, slowestWarmRender)
		}
	}
	slices.Sort(latencies)
	at := func(q float64) float64 { return latencies[int(math.Ceil(q*float64(len(latencies))))-1] }
	t.Logf("warm render over %d requests: P50 %.3f ms, P95 %.3f ms, P99 %.3f ms", len(latencies), at(.50), at(.95), at(.99))
}

// newPool returns a pool of the bundle source, closed when the test ends.
func newPool(t testing.TB, source string, cfg config.SSR) *ssr.Pool {
	t.Helper()
	p, err := ssr.NewPool(source, nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func shopConfig(vms int) config.SSR {
	return config.SSR{VMs: vms, VMMemory: 128 << 20, VMStackDepth: config.Default().SSR.VMStackDepth, Timeout: 10 * time.Second, QueueLimit: 32, WaitTimeout: time.Second}
}

// withCustomer returns the order detail input with the customer renamed. The data of each route
// is JSON text inside the input, so the name is replaced as that text spells it inside a JSON
// string. Callers check that the new name reaches the HTML.
func withCustomer(detail ssr.Input, name string) ssr.Input {
	inString := func(s string) string {
		value, _ := json.Marshal(s)
		quoted, _ := json.Marshal(string(value))
		return string(quoted[1 : len(quoted)-1])
	}
	detail.Data = json.RawMessage(strings.ReplaceAll(string(detail.Data), inString("林夏"), inString(name)))
	return detail
}

// One VM renders the same route with a new customer each time, the steps a Pool takes for each
// render. The current name must appear and the previous one must be gone. A mark the test leaves on
// the VM before the first render must still be there after each one, so every render ran in it.
func TestShopReuseKeepsRequestsApart(t *testing.T) {
	source, pages := shopBundle(t)
	cfg := shopConfig(1)
	bytecode, err := ssr.Compile(source, uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	vm, err := ssr.Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if _, err := vm.Eval("globalThis.reuseMark = 'the VM of this test'", slowjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	previous := ""
	for i := range 12 {
		name := fmt.Sprintf("customer-%03d-only", i)
		html, err := ssr.RenderVM(vm, renderInput(t, withCustomer(pages[2].input, name)), cfg.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, name) {
			t.Fatalf("request %d: missing current customer", i)
		}
		if previous != "" && strings.Contains(html, previous) {
			t.Fatalf("request %d: previous customer leaked into HTML", i)
		}
		if mark, err := vm.Eval("globalThis.reuseMark", slowjs.EvalGlobal); err != nil || mark != "the VM of this test" {
			t.Fatalf("request %d: the mark is %v, %v", i, mark, err)
		}
		previous = name
	}
}
