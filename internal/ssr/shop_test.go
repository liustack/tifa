package ssr

// Opt-in tests against the real React bundle of the prototype in examples/shop.
// Build it first, then run with TIFA_SSR_SHOP=1:
//
//	cd examples/shop && pnpm install --frozen-lockfile && pnpm build
//	TIFA_SSR_SHOP=1 go test -run Shop -v ./internal/ssr

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
)

const shopDir = "../../examples/shop"

// The prototype bundle reads its snapshot at the top level of the JSON.
// This test-only adapter unwraps Input.Data, which holds that snapshot.
const shopAdapter = `;globalThis.startRender = (start => json => start(JSON.stringify(JSON.parse(json).data)))(globalThis.startRender);`

type shopPage struct {
	input           Input
	want, forbidden string
}

// shopBundle returns the adapted prototype bundle and its four comparison pages,
// with data exported by the prototype's own Go loader.
func shopBundle(t *testing.T) (string, []shopPage) {
	t.Helper()
	if os.Getenv("TIFA_SSR_SHOP") == "" {
		t.Skip("set TIFA_SSR_SHOP=1 after running pnpm build in examples/shop")
	}
	source, err := os.ReadFile(filepath.Join(shopDir, "internal/ssr/assets/server.js"))
	if err != nil {
		t.Fatalf("run pnpm build in examples/shop first: %v", err)
	}
	fixture := filepath.Join(t.TempDir(), "snapshots.json")
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestSSRComparisonFixture$", "./internal/ssr")
	cmd.Dir = shopDir
	cmd.Env = append(os.Environ(), "SSR_COMPARE_FIXTURE="+fixture)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("export shop snapshots: %v\n%s", err, out)
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []json.RawMessage
	if err := json.Unmarshal(data, &snapshots); err != nil {
		t.Fatal(err)
	}
	var pages []shopPage
	for _, raw := range snapshots {
		var head struct{ BuildID, URL string }
		if err := json.Unmarshal(raw, &head); err != nil {
			t.Fatal(err)
		}
		want, forbidden := "SH-1042", "WK-0201"
		if strings.Contains(head.URL, "/weekend") {
			want, forbidden = forbidden, want
		}
		pages = append(pages, shopPage{Input{BuildID: head.BuildID, URL: head.URL, Data: raw}, want, forbidden})
	}
	if len(pages) != 4 {
		t.Fatalf("want 4 shop pages, got %d", len(pages))
	}
	return string(source) + shopAdapter, pages
}

func (page shopPage) check(t *testing.T, html string) {
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
	for _, text := range []string{"SH-1042", "林夏", "晨间手冲壶", "山海工作室", "data-tsr-stream-part", "正在准备操作"} {
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
