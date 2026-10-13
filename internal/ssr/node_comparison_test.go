package ssr

// Opt-in comparison with the Node and TanStack Start harness in
// .issues/2026-10-13-prototype/node-comparison. No instrumented allocator tags.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go-quick/internal/pages"
	"go-quick/internal/workspace"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func comparisonSnapshots(t *testing.T, b *Bundle) []pages.Snapshot {
	t.Helper()
	if filename := os.Getenv("SSR_COMPARE_FIXTURE_INPUT"); filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		var snapshots []pages.Snapshot
		if err := json.Unmarshal(data, &snapshots); err != nil {
			t.Fatal(err)
		}
		if len(snapshots) != 4 {
			t.Fatal("comparison requires four page fixtures")
		}
		return snapshots
	}
	loader, err := pages.New(b.Routes, b.Build.BuildID, workspace.Providers(workspace.NewStore()))
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []pages.Snapshot
	for _, url := range []string{"/projects/studio", "/projects/weekend", "/projects/studio/orders/SH-1042", "/projects/weekend/orders/WK-0201"} {
		snapshot, err := loader.Load(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

func TestSSRComparisonFixture(t *testing.T) {
	destination := os.Getenv("SSR_COMPARE_FIXTURE")
	if destination == "" {
		t.Skip("set SSR_COMPARE_FIXTURE to export identical Go data for Node")
	}
	b, err := LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(comparisonSnapshots(t, b), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSSRNodeComparison(t *testing.T) {
	mode := os.Getenv("SSR_COMPARE")
	if mode == "" {
		t.Skip("set SSR_COMPARE=reuse or async for the opt-in Node comparison")
	}
	if mode != "reuse" && mode != "async" {
		t.Fatalf("unsupported comparison policy %q", mode)
	}
	requests := experimentEnvInt(t, "REQUESTS", 2048)
	warmup := experimentEnvInt(t, "WARMUP", 512)
	if requests < 1 {
		t.Fatal("requests must be positive")
	}
	b, err := LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	snapshots := comparisonSnapshots(t, b)
	if filename := os.Getenv("SSR_COMPARE_SOURCE"); filename != "" {
		code, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		b.code = string(code)
	}
	startupStart := time.Now()
	r, err := newExperimentRenderer(b.code, experimentConfig{
		Mode: mode, Workers: 1, Spares: 1, MaxUses: 64, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	startupMS := float64(time.Since(startupStart)) / 1e6
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	render := func(index int) (Result, float64) {
		snapshot := snapshots[index%len(snapshots)]
		start := time.Now()
		result, err := r.Render(context.Background(), snapshot)
		elapsed := float64(time.Since(start)) / 1e6
		if err != nil {
			t.Fatal(err)
		}
		want, forbidden := "SH-1042", "WK-0201"
		if strings.Contains(snapshot.URL, "/weekend") {
			want, forbidden = forbidden, want
		}
		if !strings.Contains(result.HTML, want) || strings.Contains(result.HTML, forbidden) || !strings.Contains(result.HTML, "data-tsr-stream-part") {
			t.Fatalf("wrong HTML for %s", snapshot.URL)
		}
		return result, elapsed
	}
	var firstMS float64
	for i := 0; i < warmup; i++ {
		_, elapsed := render(i)
		if i == 0 {
			firstMS = elapsed
		}
	}
	r.refills.Wait()
	before := experimentMem(t)
	latencies := make([]float64, requests)
	renderTimes := make([]float64, requests)
	var htmlBytes int
	startCPU := experimentCPU(t)
	start := time.Now()
	for i := range requests {
		result, elapsed := render(i)
		latencies[i] = elapsed
		renderTimes[i] = float64(result.Render) / 1e6
		htmlBytes += len(result.HTML)
	}
	requestWall := time.Since(start)
	// Count the CPU of the last background replacement, without charging it to
	// a completed request's latency. Process startup and warmup are excluded.
	r.refills.Wait()
	settledWall := time.Since(start)
	cpu := experimentCPU(t) - startCPU
	after := experimentMem(t)
	stats := r.stats()
	if sampleDir := os.Getenv("SSR_COMPARE_SAMPLES"); sampleDir != "" {
		if err := os.MkdirAll(sampleDir, 0755); err != nil {
			t.Fatal(err)
		}
		for i := range snapshots {
			result, _ := render(i)
			if err := os.WriteFile(filepath.Join(sampleDir, fmt.Sprintf("%d.html", i)), []byte(result.HTML), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	slices.Sort(latencies)
	slices.Sort(renderTimes)
	percentile := func(values []float64, p float64) float64 { return values[int(math.Ceil(p*float64(len(values))))-1] }
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"event": "comparison", "mode": "quickjs-" + mode,
		"requests": requests, "warmup": warmup, "concurrency": 1,
		"go": runtime.Version(), "build_id": b.Build.BuildID,
		"source_bytes": len(b.code), "source_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(b.code))), "bytecode_bytes": len(r.bytecode),
		"startup_ms": startupMS, "compile_ms": float64(r.compile) / 1e6, "first_request_ms": firstMS,
		"p50_ms": percentile(latencies, .50), "p95_ms": percentile(latencies, .95), "p99_ms": percentile(latencies, .99), "max_ms": latencies[len(latencies)-1],
		"render_p50_ms": percentile(renderTimes, .50), "requests_per_s": float64(requests) / requestWall.Seconds(),
		"cpu_ms_per_request": cpu * 1e3 / float64(requests), "wall_s": requestWall.Seconds(), "settled_wall_s": settledWall.Seconds(),
		"rss_before_bytes": before.RSSBytes, "rss_after_bytes": after.RSSBytes, "go_heap_after_bytes": after.GoHeapBytes,
		"mean_html_bytes": float64(htmlBytes) / float64(requests), "stats": stats,
	}); err != nil {
		t.Fatal(err)
	}
}
