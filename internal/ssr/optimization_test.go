package ssr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"modernc.org/libc"
	"modernc.org/quickjs"
)

func TestSSRURLCompatibility(t *testing.T) {
	b, snapshot := fixture(t, "/projects/studio")
	b.code += `; globalThis.startRender = () => { try {
  const u = new URL('../orders/SH-1042?q=hello%20world&q=%E4%B8%AD#detail', 'https://example.com/projects/studio/');
  u.searchParams.append('symbol', '+ &');
  const p = new URLSearchParams([['a','1'], ['a','2']]);
  p.delete('a', '1');
  const encoded = new TextEncoder().encode('订单🙂');
  const c = new AbortController(); let events = 0;
  c.signal.addEventListener('abort', () => events++); c.abort(); c.abort();
  const response = new Response('ok', {status: 201, headers: {'X-Test': 'yes'}});
  globalThis.renderResult = JSON.stringify({html: JSON.stringify({
    pathname: u.pathname, query: u.searchParams.toString(), hash: u.hash,
    idn: new URL('https://例子.测试/').hostname, params: [...p],
    canParse: URL.canParse('/foo', 'https://example.com'),
    invalid: URL.parse('http://['), text: new TextDecoder().decode(encoded),
    aborted: c.signal.aborted, events, status: response.status, header: response.headers.get('x-test')
  })});
} catch (error) { throw new Error(error.stack); } };`
	result, err := New(b, 1, 3*time.Second).Render(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"pathname":"/projects/orders/SH-1042","query":"q=hello+world&q=%E4%B8%AD&symbol=%2B+%26","hash":"#detail","idn":"xn--fsqu00a.xn--0zwm56d","params":[["a","2"]],"canParse":true,"invalid":null,"text":"订单🙂","aborted":true,"events":1,"status":201,"header":"yes"}`
	if result.HTML != expected {
		t.Fatalf("Web API compatibility mismatch:\ngot  %s\nwant %s", result.HTML, expected)
	}
}

func TestSSRReuseAcrossRoutesAndQueryEncoding(t *testing.T) {
	b, _ := fixture(t, "/projects/studio")
	r := newTestExperiment(t, b.code, experimentConfig{Mode: "reuse", Workers: 1})
	for round := range 4 {
		for _, url := range []string{"/projects/studio?q=hello%20world", "/projects/weekend", "/projects/studio/orders/SH-1042", "/projects/weekend/orders/WK-0201"} {
			_, snapshot := fixture(t, url)
			result, err := r.Render(context.Background(), snapshot)
			if err != nil {
				t.Fatal(err)
			}
			want, forbidden := "SH-1042", "WK-0201"
			if strings.Contains(url, "weekend") {
				want, forbidden = forbidden, want
			}
			if !strings.Contains(result.HTML, want) || strings.Contains(result.HTML, forbidden) {
				t.Fatalf("route data mixed on round %d: %s", round, url)
			}
		}
	}
}

// This regression uses allocator instrumentation and forced cycle collection.
// It is separate from performance measurements and does not claim that every
// third-party WeakMap pattern is fixed by the core-js-specific adapter.
func TestSSRURLStateIsCollectible(t *testing.T) {
	if os.Getenv("SSR_MEMORY_REGRESSION") == "" {
		t.Skip("opt in with SSR_MEMORY_REGRESSION=1 and -tags memory.counters")
	}
	b, _ := fixture(t, "/projects/studio")
	if filename := os.Getenv("SSR_COMPARE_SOURCE"); filename != "" {
		code, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		b.code = string(code)
	}
	vm, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if err := vm.SetEvalTimeout(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.Eval(b.code, quickjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	if libc.MemStat().Allocs == 0 {
		t.Fatal("use -tags memory.counters")
	}
	var samples []int
	for range 3 {
		if _, err := vm.Eval(`for (let i = 0; i < 300; i++) { const u = new URL('/项目?q=a%20b', 'https://example.com'); u.searchParams.append('x', '中'); }`, quickjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			vm.SetGCThreshold(0)
			if _, err := vm.Eval("void ({})", quickjs.EvalGlobal); err != nil {
				t.Fatal(err)
			}
		}
		samples = append(samples, libc.MemStat().Allocs)
	}
	t.Logf("native allocations after 300/600/900 URLs: %v", samples)
	if samples[2] > samples[0]+8 {
		t.Fatalf("URL state retained after GC: %v", samples)
	}
}

// Only the instrumented analysis bundle registers a monotonic clock. The
// production renderer still exposes no Go callbacks to application code.
func TestSSRStageProfile(t *testing.T) {
	filename := os.Getenv("SSR_PROFILE_SOURCE")
	if filename == "" {
		t.Skip("set SSR_PROFILE_SOURCE to an instrumented analysis bundle")
	}
	b, err := LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := comparisonSnapshots(t, b)
	r := newTestExperiment(t, string(code), experimentConfig{Mode: "reuse", Workers: 1, Timeout: 10 * time.Second})
	machine := <-r.ready
	origin := time.Now()
	if err := machine.vm.RegisterFunc("__ssrNow", func() float64 { return float64(time.Since(origin)) / 1e6 }, false); err != nil {
		t.Fatal(err)
	}
	r.ready <- machine
	warmup, requests := experimentEnvInt(t, "WARMUP", 128), experimentEnvInt(t, "REQUESTS", 512)
	totals := map[string]float64{}
	for i := range warmup + requests {
		start := time.Now()
		if _, err := r.Render(context.Background(), snapshots[i%len(snapshots)]); err != nil {
			t.Fatal(err)
		}
		elapsed := float64(time.Since(start)) / 1e6
		value, err := machine.vm.Eval(`JSON.stringify(globalThis.renderProfile)`, quickjs.EvalGlobal)
		if err != nil {
			t.Fatal(err)
		}
		var profile map[string]float64
		if err := json.Unmarshal([]byte(fmt.Sprint(value)), &profile); err != nil {
			t.Fatal(err)
		}
		if len(profile) != 7 {
			t.Fatalf("missing stage timings: %v", profile)
		}
		if i >= warmup {
			var sum float64
			for phase, ms := range profile {
				totals[phase] += ms
				sum += ms
			}
			totals["host_and_bridge"] += elapsed - sum
			totals["total"] += elapsed
		}
	}
	for phase := range totals {
		totals[phase] /= float64(requests)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "stage-profile", "requests": requests, "warmup": warmup, "mean_ms": totals, "instrumented": true}); err != nil {
		t.Fatal(err)
	}
}

// Closely pair the old and new renders because unrelated workloads change
// this machine's CPU availability during the longer isolated-process runs.
func TestSSRPairedComparison(t *testing.T) {
	filename := os.Getenv("SSR_PAIR_BASELINE")
	if filename == "" {
		t.Skip("set SSR_PAIR_BASELINE to compare the archived source with the current bundle")
	}
	b, err := LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	if candidate := os.Getenv("SSR_PAIR_CANDIDATE"); candidate != "" {
		code, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatal(err)
		}
		b.code = string(code)
	}
	baseline, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := comparisonSnapshots(t, b)
	config := experimentConfig{Mode: "reuse", Workers: 1, Timeout: 10 * time.Second}
	renderers := []*experimentRenderer{newTestExperiment(t, string(baseline), config), newTestExperiment(t, b.code, config)}
	warmup, requests := experimentEnvInt(t, "WARMUP", 512), experimentEnvInt(t, "REQUESTS", 2048)
	times := [2][]float64{make([]float64, 0, requests), make([]float64, 0, requests)}
	ratios := make([]float64, 0, requests)
	for i := range warmup + requests {
		snapshot := snapshots[i%len(snapshots)]
		pair := [2]float64{}
		first := (i / len(snapshots)) % 2
		for _, variant := range []int{first, 1 - first} {
			start := time.Now()
			result, err := renderers[variant].Render(context.Background(), snapshot)
			pair[variant] = float64(time.Since(start)) / 1e6
			if err != nil {
				t.Fatal(err)
			}
			want, forbidden := "SH-1042", "WK-0201"
			if strings.Contains(snapshot.URL, "weekend") {
				want, forbidden = forbidden, want
			}
			if !strings.Contains(result.HTML, want) || strings.Contains(result.HTML, forbidden) {
				t.Fatalf("wrong paired HTML: variant %d, %s", variant, snapshot.URL)
			}
		}
		if i >= warmup {
			for variant := range times {
				times[variant] = append(times[variant], pair[variant])
			}
			ratios = append(ratios, pair[1]/pair[0])
		}
	}
	for variant := range times {
		slices.Sort(times[variant])
	}
	slices.Sort(ratios)
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"event": "paired-comparison", "pairs": requests, "warmup_pairs": warmup,
		"baseline_p50_ms": times[0][requests/2], "optimized_p50_ms": times[1][requests/2],
		"median_paired_optimized_over_baseline":   ratios[requests/2],
		"median_paired_latency_reduction_percent": 100 * (1 - ratios[requests/2]),
	}); err != nil {
		t.Fatal(err)
	}
}
