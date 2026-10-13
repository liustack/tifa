package ssr

// A stdin-controlled, opt-in harness keeps all compared engines warm while an
// external driver alternates small batches. Only one engine is active at once.
import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSSRControlledComparison(t *testing.T) {
	if os.Getenv("SSR_COMPARE_CONTROL") == "" {
		t.Skip("opt-in interleaved engine comparison")
	}
	b, err := LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	snapshots := comparisonSnapshots(t, b)
	startup := time.Now()
	r := newTestExperiment(t, b.code, experimentConfig{Mode: "reuse", Workers: 1, Timeout: 10 * time.Second})
	startupMS := float64(time.Since(startup)) / 1e6
	render := func(i int) (Result, float64) {
		snapshot := snapshots[i%len(snapshots)]
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
	warmup := experimentEnvInt(t, "WARMUP", 512)
	for i := range warmup {
		render(i)
	}
	encoder := json.NewEncoder(os.Stdout)
	emit := func(v any) {
		if err := encoder.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	emit(map[string]any{
		"event": "ready", "go": runtime.Version(), "pid": os.Getpid(), "warmup": warmup,
		"startup_ms": startupMS, "compile_ms": float64(r.compile) / 1e6,
		"source_bytes": len(b.code), "source_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(b.code))),
		"bytecode_bytes": len(r.bytecode), "stats": r.stats(), "memory": experimentMem(t),
	})
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command struct {
			Action    string `json:"action"`
			ID        string `json:"id"`
			Requests  int    `json:"requests"`
			Directory string `json:"directory"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
			t.Fatal(err)
		}
		switch command.Action {
		case "measure":
			if command.Requests < 1 || command.Requests > 8192 || command.Requests%len(snapshots) != 0 {
				t.Fatal("batch must contain 1..8192 renders and complete fixture cycles")
			}
			latencies := make([]float64, command.Requests)
			var htmlBytes int
			cpu := experimentCPU(t)
			start := time.Now()
			for i := range command.Requests {
				result, elapsed := render(i)
				latencies[i] = elapsed
				htmlBytes += len(result.HTML)
			}
			wall := time.Since(start)
			cpu = experimentCPU(t) - cpu
			emit(map[string]any{"event": "batch", "id": command.ID, "requests": command.Requests,
				"latencies_ms": latencies, "wall_s": wall.Seconds(), "cpu_s": cpu, "html_bytes": htmlBytes})
		case "memory":
			emit(map[string]any{"event": "memory", "memory": experimentMem(t), "stats": r.stats()})
		case "samples":
			if err := os.MkdirAll(command.Directory, 0755); err != nil {
				t.Fatal(err)
			}
			for i := range snapshots {
				result, _ := render(i)
				if err := os.WriteFile(filepath.Join(command.Directory, fmt.Sprintf("%d.html", i)), []byte(result.HTML), 0644); err != nil {
					t.Fatal(err)
				}
			}
			emit(map[string]any{"event": "samples"})
		case "close":
			emit(map[string]any{"event": "closed"})
			return
		default:
			t.Fatalf("unknown benchmark command %q", command.Action)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}
