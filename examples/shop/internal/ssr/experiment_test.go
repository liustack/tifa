package ssr

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/liustack/tifa/internal/pages"
	"github.com/liustack/tifa/internal/workspace"
	"math"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"modernc.org/libc"
	"modernc.org/quickjs"
)

type experimentMemory struct {
	NativeAllocations int    `json:"native_live_allocations"`
	NativeMappedBytes int    `json:"native_mapped_bytes"`
	NativeMappings    int    `json:"native_mappings"`
	GoHeapBytes       uint64 `json:"go_heap_bytes"`
	GoHeapObjects     uint64 `json:"go_heap_objects"`
	GoGCs             uint32 `json:"go_gcs"`
	RSSBytes          int64  `json:"rss_bytes"`
	PeakRSSBytes      int64  `json:"peak_rss_bytes"`
	Goroutines        int    `json:"goroutines"`
}

func experimentMem(t *testing.T) experimentMemory {
	t.Helper()
	// Native QuickJS memory is separate from Go's heap. Go GC here makes the
	// checkpoint comparable but does not force QuickJS to collect cycles.
	runtime.GC()
	var goMem runtime.MemStats
	runtime.ReadMemStats(&goMem)
	native := libc.MemStat()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	peak := usage.Maxrss
	if runtime.GOOS == "linux" {
		peak *= 1024
	}
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Fatal(err)
	}
	rss, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return experimentMemory{NativeAllocations: native.Allocs, NativeMappedBytes: native.Bytes, NativeMappings: native.Mmaps, GoHeapBytes: goMem.HeapAlloc, GoHeapObjects: goMem.HeapObjects, GoGCs: goMem.NumGC, RSSBytes: rss * 1024, PeakRSSBytes: peak, Goroutines: runtime.NumGoroutine()}
}

func experimentCPU(t *testing.T) float64 {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	return float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
}

func experimentEnvInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := os.Getenv("SSR_" + name)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		t.Fatalf("invalid %s=%q", name, value)
	}
	return n
}

// Opt-in subprocess harness. Each policy must run in a fresh process so that
// allocator caches, Go GC and peak RSS from one policy do not affect another.
func TestSSRExperiment(t *testing.T) {
	mode := os.Getenv("SSR_MODE")
	if mode == "" {
		t.Skip("opt in with SSR_MODE and compile with -tags memory.counters")
	}
	requests := experimentEnvInt(t, "REQUESTS", 300)
	clients := experimentEnvInt(t, "CLIENTS", 2)
	workers := experimentEnvInt(t, "WORKERS", 2)
	sampleEvery := experimentEnvInt(t, "SAMPLE_EVERY", 0)
	if requests == 0 || clients == 0 {
		t.Fatal("requests and clients must be positive")
	}
	if sampleEvery == 0 {
		sampleEvery = requests
	}
	cfg := experimentConfig{Mode: mode, Workers: workers, Spares: experimentEnvInt(t, "SPARES", 1), MaxUses: experimentEnvInt(t, "MAX_USES", 64), CollectEvery: experimentEnvInt(t, "COLLECT_EVERY", 0), Timeout: 10 * time.Second}
	b, err := LoadBundle()
	if err != nil {
		t.Fatal(err)
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
	scenario := os.Getenv("SSR_SCENARIO")
	if scenario == "" {
		scenario = "react"
	}
	code := experimentSource(t, b.code)
	if scenario == "retained" {
		// Positive control: an explicit module-level reference prevents GC from
		// freeing 256 KiB per request, even though rendering itself succeeds.
		code += `; const originalStart = globalThis.startRender; const retained = []; globalThis.startRender = input => { const bytes = new Uint8Array(256 * 1024); bytes.fill(1); retained.push(bytes); originalStart(input); };`
	} else if scenario == "property-state" {
		code = experimentPropertyState(t, code)
	} else if scenario != "react" {
		t.Fatalf("unknown scenario %q", scenario)
	}

	// Establish the allocator's process-level baseline after one empty VM.
	vm, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	if libc.MemStat().Allocs == 0 {
		t.Fatal("rebuild test binary with -tags memory.counters")
	}
	if err := vm.Close(); err != nil {
		t.Fatal(err)
	}
	baseline := experimentMem(t)
	encoder := json.NewEncoder(os.Stdout)
	emit := func(value any) {
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	emit(map[string]any{"event": "baseline", "mode": mode, "scenario": scenario, "memory": baseline, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "build_id": b.Build.BuildID, "source_bytes": len(code), "workers": workers, "clients": clients, "max_uses": cfg.MaxUses, "collect_every": cfg.CollectEvery})
	startCPU := experimentCPU(t)
	start := time.Now()
	r, err := newExperimentRenderer(code, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	startup := time.Since(start)
	emit(map[string]any{"event": "ready", "startup_ms": float64(startup) / 1e6, "compile_ms": float64(r.compile) / 1e6, "bytecode_bytes": len(r.bytecode), "stats": r.stats(), "memory": experimentMem(t)})

	latencies := make([]float64, requests)
	renderTimes := make([]float64, requests)
	var failuresMu sync.Mutex
	var failures []string
	var renderWall time.Duration
	for offset := 0; offset < requests; offset += sampleEvery {
		end := min(requests, offset+sampleEvery)
		jobs := make(chan int)
		var wg sync.WaitGroup
		batchStart := time.Now()
		for range clients {
			wg.Go(func() {
				for i := range jobs {
					snapshot := snapshots[i%len(snapshots)]
					requestStart := time.Now()
					result, err := r.Render(context.Background(), snapshot)
					latencies[i] = float64(time.Since(requestStart)) / 1e6
					renderTimes[i] = float64(result.Render) / 1e6
					want, forbidden := "SH-1042", "WK-0201"
					if strings.Contains(snapshot.URL, "/weekend") {
						want, forbidden = forbidden, want
					}
					if err == nil && (!strings.Contains(result.HTML, want) || strings.Contains(result.HTML, forbidden) || !strings.Contains(result.HTML, "data-tsr-stream-part")) {
						err = fmt.Errorf("wrong SSR data for %s", snapshot.URL)
					}
					if err != nil {
						failuresMu.Lock()
						failures = append(failures, fmt.Sprintf("request %d: %v", i, err))
						failuresMu.Unlock()
					}
				}
			})
		}
		for i := offset; i < end; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		renderWall += time.Since(batchStart)
		// Quiesce background replenishment before measuring live allocations.
		// The time to do so remains in total wall/CPU, outside request latency.
		r.refills.Wait()
		emit(map[string]any{"event": "sample", "requests": end, "stats": r.stats(), "memory": experimentMem(t), "cpu_s": experimentCPU(t) - startCPU})
	}
	closeStart := time.Now()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	closeTime := time.Since(closeStart)
	endMemory := experimentMem(t)
	wall := time.Since(start)
	cpu := experimentCPU(t) - startCPU
	slices.Sort(latencies)
	slices.Sort(renderTimes)
	percentile := func(values []float64, p float64) float64 { return values[int(math.Ceil(p*float64(len(values))))-1] }
	emit(map[string]any{"event": "summary", "mode": mode, "scenario": scenario, "requests": requests, "failures": failures, "startup_ms": float64(startup) / 1e6, "close_ms": float64(closeTime) / 1e6, "wall_s": wall.Seconds(), "render_wall_s": renderWall.Seconds(), "requests_per_s": float64(requests) / renderWall.Seconds(), "total_requests_per_s": float64(requests) / wall.Seconds(), "cpu_s": cpu, "cpu_ms_per_request": cpu * 1e3 / float64(requests), "p50_ms": percentile(latencies, .50), "p95_ms": percentile(latencies, .95), "p99_ms": percentile(latencies, .99), "max_ms": latencies[len(latencies)-1], "render_p50_ms": percentile(renderTimes, .50), "stats": r.stats(), "memory": endMemory, "native_allocation_delta_after_close": endMemory.NativeAllocations - baseline.NativeAllocations})
	if len(failures) != 0 {
		t.Fatalf("%d render failures", len(failures))
	}
	if stats := r.stats(); stats.Live != 0 || stats.Created != stats.Closed {
		t.Fatalf("VM lifecycle leak: %+v", stats)
	}
	if endMemory.NativeAllocations != baseline.NativeAllocations {
		t.Fatalf("native allocations did not return to baseline: before %d, after %d", baseline.NativeAllocations, endMemory.NativeAllocations)
	}
}
