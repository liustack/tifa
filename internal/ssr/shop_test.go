package ssr

// Opt-in tests against the real React bundle of the example app in examples/shop.
// Build it first, then run with TIFA_SSR_SHOP=1:
//
//	cd examples/shop && pnpm install --frozen-lockfile && go tool tifa build
//	TIFA_SSR_SHOP=1 go test -run Shop -v ./internal/ssr

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

	"github.com/liustack/tifa/internal/config"
)

const shopDir = "../../examples/shop"

type shopPage struct {
	input           Input
	want, forbidden string
}

// shopRoutes are the four comparison pages of the prototype baseline, a list and a detail page
// of each project, with the route IDs of their match chains.
var shopRoutes = []struct{ url, routes string }{
	{"/projects/studio", "app/layout,app/projects/project+/layout,app/projects/project+/route"},
	{"/projects/weekend", "app/layout,app/projects/project+/layout,app/projects/project+/route"},
	{"/projects/studio/orders/SH-1042", "app/layout,app/projects/project+/layout,app/projects/project+/orders/order+/route"},
	{"/projects/weekend/orders/WK-0201", "app/layout,app/projects/project+/layout,app/projects/project+/orders/order+/route"},
}

// shopBundle returns the shop's SSR bundle and its four comparison pages. The data of each
// page is what the shop's own program injects: the test runs bin/shop and asks it for the
// navigation data of each page, which has the same form as the injected data.
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
		want, forbidden := "SH-1042", "WK-0201"
		if strings.Contains(r.url, "/weekend") {
			want, forbidden = forbidden, want
		}
		pages = append(pages, shopPage{Input{BuildID: built.BuildID, URL: r.url, Data: body}, want, forbidden})
	}
	return string(source), pages
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
	if !strings.Contains(html, page.want) || strings.Contains(html, page.forbidden) || !strings.Contains(html, "data-tsr-stream-part") {
		t.Fatalf("wrong HTML for %s", page.input.URL)
	}
}

func TestShopRendersReadyData(t *testing.T) {
	source, pages := shopBundle(t)
	p := newTestPool(t, source, shopConfig(1))
	detail := pages[2]
	if detail.input.URL != "/projects/studio/orders/SH-1042" {
		t.Fatalf("unexpected page order: %s", detail.input.URL)
	}
	html, err := p.Render(context.Background(), detail.input)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"SH-1042", "林夏", "晨间手冲壶", ">山海工作室</h1>", "2</span> 单待发货", "data-tsr-stream-part", "正在准备操作"} {
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
	p := newTestPool(t, source, shopConfig(1))
	in := pages[2].input
	in.Data = json.RawMessage(strings.ReplaceAll(string(in.Data), `"林夏"`, `"</script><script id=\"injected\">alert(1)</script>"`))
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

// Same method as the prototype baseline: one reused VM, 512 warm renders,
// then 2,048 timed renders over the four pages. Timing covers the whole Render.
func TestShopWarmLatency(t *testing.T) {
	source, pages := shopBundle(t)
	start := time.Now()
	p := newTestPool(t, source, shopConfig(1))
	t.Logf("startup (compile and load one VM): %s, bytecode %d bytes", time.Since(start), len(p.bytecode))
	for i := range 512 {
		page := pages[i%len(pages)]
		html, err := p.Render(context.Background(), page.input)
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
	}
	latencies := make([]float64, 2048)
	for i := range latencies {
		page := pages[i%len(pages)]
		begin := time.Now()
		html, err := p.Render(context.Background(), page.input)
		latencies[i] = float64(time.Since(begin)) / 1e6
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
	}
	slices.Sort(latencies)
	at := func(q float64) float64 { return latencies[int(math.Ceil(q*float64(len(latencies))))-1] }
	t.Logf("warm render over %d requests: P50 %.3f ms, P95 %.3f ms, P99 %.3f ms", len(latencies), at(.50), at(.95), at(.99))
}

func shopConfig(vms int) config.SSR {
	return config.SSR{VMs: vms, VMMemory: 128 << 20, Timeout: 10 * time.Second}
}

// withCustomer returns the order detail input with the customer renamed.
// Callers check that the new name reaches the HTML.
func withCustomer(detail Input, name string) Input {
	detail.Data = json.RawMessage(strings.ReplaceAll(string(detail.Data), `"林夏"`, `"`+name+`"`))
	return detail
}

// One reused VM renders the same route with a new customer each time.
// The current name must appear and the previous one must be gone.
func TestShopReuseKeepsRequestsApart(t *testing.T) {
	source, pages := shopBundle(t)
	p := newTestPool(t, source, shopConfig(1))
	previous := ""
	for i := range 12 {
		name := fmt.Sprintf("customer-%03d-only", i)
		html, err := p.Render(context.Background(), withCustomer(pages[2].input, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, name) {
			t.Fatalf("request %d: missing current customer", i)
		}
		if previous != "" && strings.Contains(html, previous) {
			t.Fatalf("request %d: previous customer leaked into HTML", i)
		}
		previous = name
	}
	if s := p.stats(); s.created != 1 {
		t.Fatalf("want one reused VM, got %+v", s)
	}
}
