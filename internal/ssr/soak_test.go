package ssr

// Opt-in long tests of VM reuse and OOM recycling. They count QuickJS native
// allocations, so they need the memory.counters build tag:
//
//	TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr
//
// Add TIFA_SSR_SHOP=1 to run them against the real React bundle as well (see shop_test.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/quickjs"
	lib "github.com/liustack/quickjs/lib"
	"github.com/liustack/tifa/internal/config"
	"modernc.org/libc"
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

// Two VMs, two callers, 10,000 renders. Every render must carry its own data,
// no VM may be retired, and closing the pool returns native memory to the baseline.
func TestSoakReuse(t *testing.T) {
	for _, b := range soakBundles(t) {
		t.Run(b.name, func(t *testing.T) {
			baseline := nativeBaseline(t)
			cfg := config.SSR{VMs: 2, VMMemory: 128 << 20, Timeout: 10 * time.Second}
			p, err := NewPool(b.source, nil, cfg, false)
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
						// Every request carries its own marker, as the text echoed by the
						// testdata page or as the customer of the shop's order detail page.
						marker := fmt.Sprintf("request-%05d-only", i)
						previous := fmt.Sprintf("request-%05d-only", i-len(b.pages))
						switch {
						case b.text:
							in = input("garbage", marker)
							previous = fmt.Sprintf("request-%05d-only", i-1)
						case i%len(b.pages) == b.heavy:
							in = withCustomer(in, marker)
						default:
							marker, previous = "", ""
						}
						html, err := p.Render(context.Background(), in)
						if err != nil {
							t.Errorf("request %d: %v", i, err)
							continue
						}
						if !strings.Contains(html, marker) {
							t.Errorf("request %d: missing its own data", i)
						}
						if i >= len(b.pages) && previous != "" && strings.Contains(html, previous) {
							t.Errorf("request %d: data of an earlier request leaked into HTML", i)
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
			p, err := NewPool(b.source+retain, nil, config.SSR{VMs: 1, VMMemory: 32 << 20, Timeout: 10 * time.Second}, false)
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

// What Intl adds to a VM after a GC: loaded without Intl as before, loaded by load, and after a
// page formats a number and a date. It gives two counts of QuickJS: malloc, the bytes the VM's
// allocations take, which ssr.vm_memory limits, and used, its own estimate of what its objects,
// strings and functions take. Intl only puts stand-ins on a VM until it is used, so a VM whose
// pages never format may cost a few KB more.
func TestSoakVMMemory(t *testing.T) {
	const formats = `[new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY'}).format(1234.5),
		new Date(0).toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'})].join()`
	for _, b := range soakBundles(t) {
		t.Run(b.name, func(t *testing.T) {
			const memory, timeout = 128 << 20, 10 * time.Second
			bytecode, err := compile(b.source, memory, timeout)
			if err != nil {
				t.Fatal(err)
			}
			// measure returns what a VM from open holds, after use when it is set.
			measure := func(open func() (*quickjs.VM, error), use func(*quickjs.VM)) lib.TJSMemoryUsage {
				vm, err := open()
				if err != nil {
					t.Fatal(err)
				}
				defer vm.Close()
				if use != nil {
					use(vm)
				}
				return vmMemory(vm, true)
			}
			withoutIntl := func() (*quickjs.VM, error) {
				vm, err := quickjs.NewVM()
				if err != nil {
					return nil, err
				}
				vm.SetMemoryLimit(memory)
				if _, err := vm.EvalBytecode(bytecode); err != nil {
					return nil, errors.Join(err, vm.Close())
				}
				return vm, nil
			}
			withIntl := func() (*quickjs.VM, error) { return load(bytecode, memory, timeout, false) }
			format := func(vm *quickjs.VM) {
				if _, err := vm.Eval(formats, quickjs.EvalGlobal); err != nil {
					t.Fatal(err)
				}
			}
			render := func(vm *quickjs.VM) {
				in, err := json.Marshal(b.pages[0].input)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := render(vm, in, timeout); err != nil {
					t.Fatal(err)
				}
			}
			plain := measure(withoutIntl, nil)
			kb := func(n int64) float64 { return float64(n) / 1024 }
			show := func(name string, u, base lib.TJSMemoryUsage) {
				t.Logf("%-28s malloc %7.1f KB (%+5.1f), used %7.1f KB (%+5.1f)", name,
					kb(u.Fmalloc_size), kb(u.Fmalloc_size-base.Fmalloc_size),
					kb(u.Fmemory_used_size), kb(u.Fmemory_used_size-base.Fmemory_used_size))
			}
			unused := measure(withIntl, nil)
			show("without Intl", plain, plain)
			show("with Intl, unused", unused, plain)
			show("with Intl, after formatting", measure(withIntl, format), plain)
			rendered := measure(withoutIntl, render)
			show("rendered without Intl", rendered, rendered)
			show("rendered with Intl", measure(withIntl, render), rendered)
			if grown := unused.Fmalloc_size - plain.Fmalloc_size; grown > 32<<10 {
				t.Fatalf("Intl adds %.1f KB to a VM that never formats", kb(grown))
			}
		})
	}
}
