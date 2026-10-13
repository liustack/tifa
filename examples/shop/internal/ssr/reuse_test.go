package ssr

import (
	"context"
	"fmt"
	"github.com/liustack/tifa/examples/shop/internal/pages"
	"github.com/liustack/tifa/examples/shop/internal/workspace"
	"strings"
	"sync"
	"testing"
	"time"
)

const counterProgram = `let requests = 0; globalThis.startRender = () => { globalThis.renderResult = JSON.stringify({html: String(++requests)}); };`

func TestExperimentReuseAndRecycle(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want []string
	}{
		{"source", []string{"1", "1", "1", "1", "1"}},
		{"bytecode", []string{"1", "1", "1", "1", "1"}},
		{"reuse", []string{"1", "2", "3", "4", "5"}},
		{"sync", []string{"1", "2", "1", "2", "1"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			r := newTestExperiment(t, counterProgram, experimentConfig{Mode: tc.mode, Workers: 1, MaxUses: 2})
			for _, want := range tc.want {
				got, err := r.Render(context.Background(), pages.Snapshot{})
				if err != nil || got.HTML != want {
					t.Fatalf("got %q, %v, want %q", got.HTML, err, want)
				}
			}
		})
	}
}

func TestExperimentActualReactReuse(t *testing.T) {
	b, snapshot := fixture(t, "/projects/studio/orders/SH-1042")
	r := newTestExperiment(t, b.code, experimentConfig{Mode: "reuse", Workers: 1})
	for i := range 12 {
		order := snapshot.Scopes["page:projects/[projectId]/orders/[orderId]"]["order"].(workspace.Order)
		order.Customer = fmt.Sprintf("customer-%03d-only", i)
		snapshot.Scopes["page:projects/[projectId]/orders/[orderId]"]["order"] = order
		result, err := r.Render(context.Background(), snapshot)
		if err != nil || !strings.Contains(result.HTML, order.Customer) {
			t.Fatalf("request %d: missing current customer, %v", i, err)
		}
		if i > 0 && strings.Contains(result.HTML, fmt.Sprintf("customer-%03d-only", i-1)) {
			t.Fatal("previous request's customer leaked into HTML")
		}
	}
	if got := r.stats().Created; got != 1 {
		t.Fatalf("expected one reused VM, created %d", got)
	}
}

func TestExperimentFailedVMIsReplaced(t *testing.T) {
	code := `let dirty = false; globalThis.startRender = input => {
		const mode = JSON.parse(input).title;
		if (mode !== 'ok') dirty = true;
		if (mode === 'throw') throw new Error('intentional');
		if (mode === 'loop') { while (true) {} }
		if (mode === 'oom') new Uint8Array(128 * 1024 * 1024);
		if (mode === 'pending') return;
		if (mode === 'jobs') { Promise.resolve().then(() => { while (true) {} }); return; }
		globalThis.renderResult = JSON.stringify({html: dirty ? 'dirty' : 'clean'});
	};`
	for _, mode := range []string{"reuse", "sync", "async", "single"} {
		t.Run(mode, func(t *testing.T) {
			r := newTestExperiment(t, code, experimentConfig{Mode: mode, Workers: 1, Spares: 1, MaxUses: 64, MemoryLimit: 2 << 20, Timeout: 50 * time.Millisecond})
			for _, failure := range []string{"throw", "pending", "loop", "jobs", "oom"} {
				if _, err := r.Render(context.Background(), pages.Snapshot{Title: failure}); err == nil {
					t.Fatalf("accepted %s", failure)
				}
				got, err := r.Render(context.Background(), pages.Snapshot{Title: "ok"})
				if err != nil || got.HTML != "clean" {
					t.Fatalf("failed VM was reused: %q, %v", got.HTML, err)
				}
			}
		})
	}
}

func TestExperimentAsyncBoundedAndClosed(t *testing.T) {
	code := `globalThis.startRender = input => { const name = JSON.parse(input).title; Promise.resolve().then(() => { globalThis.renderResult = JSON.stringify({html: name}); }); };`
	r := newTestExperiment(t, code, experimentConfig{Mode: "async", Workers: 2, Spares: 1, MaxUses: 3})
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			name := fmt.Sprintf("request-%d", i)
			got, err := r.Render(context.Background(), pages.Snapshot{Title: name})
			if err != nil || got.HTML != name {
				t.Errorf("want %s, got %s, %v", name, got.HTML, err)
			}
		})
	}
	wg.Wait()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	stats := r.stats()
	if stats.Live != 0 || stats.Created != stats.Closed || stats.PeakLive > 3 {
		t.Fatalf("unbounded or unfinished lifecycle: %+v", stats)
	}
	if _, err := r.Render(context.Background(), pages.Snapshot{}); err == nil {
		t.Fatal("render after close succeeded")
	}
}

func TestExperimentShutdownWhileBusy(t *testing.T) {
	r := newTestExperiment(t, `globalThis.startRender = () => { while (true) {} };`, experimentConfig{Mode: "async", Workers: 1, Spares: 1, MaxUses: 1, Timeout: 60 * time.Millisecond})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _, _ = r.Render(context.Background(), pages.Snapshot{}) })
	}
	deadline := time.Now().Add(time.Second)
	for len(r.slots) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("render did not start")
		}
		time.Sleep(time.Millisecond)
	}
	wg.Go(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	wg.Go(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	if stats := r.stats(); stats.Live != 0 || stats.Created != stats.Closed {
		t.Fatalf("shutdown leaked VM: %+v", stats)
	}
}

func newTestExperiment(t *testing.T, code string, cfg experimentConfig) *experimentRenderer {
	t.Helper()
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Second
	}
	r, err := newExperimentRenderer(code, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}
