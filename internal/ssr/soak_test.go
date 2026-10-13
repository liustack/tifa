package ssr

// Opt-in long tests of VM reuse and OOM recycling. They count QuickJS native
// allocations, so they need the memory.counters build tag:
//
//	TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr
//
// Add TIFA_SSR_SHOP=1 to run them against the real React bundle as well (see shop_test.go).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/config"
	"modernc.org/libc"
	"modernc.org/quickjs"
)

type soakBundle struct {
	name   string
	source string
	pages  []shopPage // the request for index i is pages[i%len(pages)]
	text   bool       // pages echo data.text, so each request can carry its own marker
	heavy  int        // index of the page used to drive a VM out of memory
}

func soakBundles(t *testing.T) []soakBundle {
	t.Helper()
	if os.Getenv("TIFA_SSR_SOAK") == "" {
		t.Skip("set TIFA_SSR_SOAK=1 and build with -tags memory.counters")
	}
	bundles := []soakBundle{{name: "testdata", source: testSource(t), pages: []shopPage{{input: input("ok", "")}}, text: true}}
	if os.Getenv("TIFA_SSR_SHOP") != "" {
		source, pages := shopBundle(t)
		bundles = append(bundles, soakBundle{name: "shop", source: source, pages: pages, heavy: 2})
	}
	return bundles
}

// nativeBaseline returns the live native allocations after one VM has been
// created and closed, which warms the allocator like the prototype measurements.
func nativeBaseline(t *testing.T) int {
	t.Helper()
	vm, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.Close(); err != nil {
		t.Fatal(err)
	}
	n := libc.MemStat().Allocs
	if n == 0 {
		t.Fatal("native allocation counters are off, build with -tags memory.counters")
	}
	return n
}

func peakRSS(t *testing.T) string {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	peak := usage.Maxrss // bytes on darwin, KiB on linux
	if runtime.GOOS == "linux" {
		peak *= 1024
	}
	return fmt.Sprintf("%.1f MiB", float64(peak)/(1<<20))
}

// Two VMs, two callers, 10,000 renders. Every render must carry its own data,
// no VM may be retired, and closing the pool returns native memory to the baseline.
func TestSoakReuse(t *testing.T) {
	for _, b := range soakBundles(t) {
		t.Run(b.name, func(t *testing.T) {
			baseline := nativeBaseline(t)
			cfg := config.SSR{VMs: 2, VMMemory: 128 << 20, Timeout: 10 * time.Second}
			p, err := NewPool(b.source, cfg)
			if err != nil {
				t.Fatal(err)
			}
			const requests, callers = 10_000, 2
			jobs := make(chan int)
			var wg sync.WaitGroup
			for range callers {
				wg.Go(func() {
					for i := range jobs {
						page := b.pages[i%len(b.pages)]
						in := page.input
						marker := fmt.Sprintf("request-%05d-only", i)
						if b.text {
							in = input("garbage", marker)
						}
						html, err := p.Render(context.Background(), in)
						if err != nil {
							t.Errorf("request %d: %v", i, err)
							continue
						}
						if b.text && !strings.Contains(html, marker) {
							t.Errorf("request %d: missing its own data", i)
						}
						if !b.text && (!strings.Contains(html, page.want) || strings.Contains(html, page.forbidden)) {
							t.Errorf("request %d: wrong HTML for %s", i, page.input.URL)
						}
					}
				})
			}
			for i := range requests {
				jobs <- i
				if (i+1)%2_000 == 0 {
					t.Logf("%d renders, native allocations %d, peak RSS %s", i+1, libc.MemStat().Allocs, peakRSS(t))
				}
			}
			close(jobs)
			wg.Wait()
			if s := p.stats(); s.created != cfg.VMs || s.closed != 0 {
				t.Fatalf("VMs were retired during normal reuse: %+v", s)
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			if after := libc.MemStat().Allocs; after != baseline {
				t.Fatalf("native allocations after close: baseline %d, after %d", baseline, after)
			}
			t.Logf("native allocations back to baseline %d", baseline)
		})
	}
}

// Each render keeps 256 KiB alive until the VM hits its 32 MiB limit. The pool
// must replace the VM, serve the next render from a fresh one, and after eight
// such cycles and Close, native memory must be back at the baseline.
func TestSoakOOMRecycle(t *testing.T) {
	const retain = `;globalThis.startRender = (start => { const retained = []; return json => { retained.push(new Uint8Array(256 * 1024).fill(1)); start(json) } })(globalThis.startRender);`
	for _, b := range soakBundles(t) {
		t.Run(b.name, func(t *testing.T) {
			baseline := nativeBaseline(t)
			p, err := NewPool(b.source+retain, config.SSR{VMs: 1, VMMemory: 32 << 20, Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			// For the shop this is the order detail page, the page the prototype's OOM probe used.
			page := b.pages[b.heavy]
			for cycle := range 8 {
				before := p.stats()
				failed := false
				for i := range 256 {
					_, err := p.Render(context.Background(), page.input)
					if err == nil {
						continue
					}
					// Only the memory limit may end a cycle. A timeout, a page error or
					// a bare null (QuickJS out of room even for its error object) fails
					// the test. With these fixed sizes the run is deterministic.
					if !errors.Is(err, ErrOutOfMemory) {
						t.Fatalf("cycle %d request %d: want ErrOutOfMemory, got %v", cycle, i, err)
					}
					if i == 0 {
						t.Fatalf("cycle %d: first render of a fresh VM ran out of memory", cycle)
					}
					failed = true
					t.Logf("cycle %d: request %d failed: %v", cycle+1, i+1, err)
					break
				}
				if !failed {
					t.Fatal("VM never reached its memory limit")
				}
				if _, err := p.Render(context.Background(), page.input); err != nil {
					t.Fatalf("cycle %d: render after OOM: %v", cycle, err)
				}
				if s := p.stats(); s.created != before.created+1 || s.closed != before.closed+1 {
					t.Fatalf("cycle %d: OOM VM was not replaced, before %+v after %+v", cycle, before, s)
				}
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			if after := libc.MemStat().Allocs; after != baseline {
				t.Fatalf("OOM recycling retained native allocations: baseline %d, after %d", baseline, after)
			}
			t.Logf("native allocations back to baseline %d", baseline)
		})
	}
}
