package ssr

// Opt-in resident-memory experiment. Run each VM count in a fresh process,
// using the same uninstrumented binary as the latency comparison.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSSRVMFootprint(t *testing.T) {
	if os.Getenv("SSR_VM_FOOTPRINT") == "" {
		t.Skip("opt-in VM memory scaling experiment")
	}
	workers := experimentEnvInt(t, "WORKERS", 1)
	renders := experimentEnvInt(t, "RENDERS_PER_VM", 512)
	if workers < 1 || workers > 64 || renders < 4 || renders%4 != 0 {
		t.Fatal("require 1..64 VMs and a positive multiple of four renders per VM")
	}
	b, err := LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	snapshots := comparisonSnapshots(t, b)
	encoder := json.NewEncoder(os.Stdout)
	emit := func(value any) {
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	emit(map[string]any{
		"event": "baseline", "pid": os.Getpid(), "workers": workers, "spares": 0,
		"concurrency": 1, "renders_per_vm": renders, "native_counters": false,
		"source_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(b.code))),
		"memory":        experimentMem(t),
	})
	r := newTestExperiment(t, b.code, experimentConfig{Mode: "reuse", Workers: workers, Timeout: 10 * time.Second})
	emit(map[string]any{"event": "loaded", "bytecode_bytes": len(r.bytecode), "stats": r.stats(), "memory": experimentMem(t)})
	for request := range renders {
		// A serial pass rotates through every VM in the FIFO. Each VM sees all
		// fixtures, even when the VM count is a multiple of the fixture count.
		snapshot := snapshots[request%len(snapshots)]
		for range workers {
			result, err := r.Render(context.Background(), snapshot)
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
		}
		if request+1 == 128 || request+1 == renders {
			emit(map[string]any{"event": "warm", "renders_per_vm": request + 1, "requests": (request + 1) * workers, "stats": r.stats(), "memory": experimentMem(t)})
		}
	}
	for range workers {
		machine := <-r.ready
		if machine.uses != renders {
			t.Errorf("VM rendered %d requests, want %d", machine.uses, renders)
		}
		r.ready <- machine
	}
	stats := r.stats()
	if stats.Created != int64(workers) || stats.Live != int64(workers) || stats.PeakLive != int64(workers) || stats.Retired != 0 {
		t.Fatalf("unexpected VM population: %+v", stats)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	stats = r.stats()
	if stats.Live != 0 || stats.Created != stats.Closed {
		t.Fatalf("VMs remain after close: %+v", stats)
	}
	emit(map[string]any{"event": "closed", "stats": stats, "memory": experimentMem(t)})
	runtime.KeepAlive(snapshots)
	runtime.KeepAlive(b)
}
