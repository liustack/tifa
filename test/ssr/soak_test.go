package ssr_test

// Opt-in long tests of VM reuse and OOM recycling with the shop's bundle, the same as those of
// internal/ssr with its testdata bundle. They count QuickJS native allocations, so they need the
// memory.counters build tag:
//
//	TIFA_SSR_SHOP=1 TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./test/ssr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/liustack/slowjs"
	lib "github.com/liustack/slowjs/lib"
	"github.com/liustack/tifa/internal/ssr"
	"modernc.org/libc"
)

// soakBundle returns the shop's bundle and pages, or skips the test unless both TIFA_SSR_SOAK and
// TIFA_SSR_SHOP are set.
func soakBundle(t *testing.T) (string, []shopPage) {
	t.Helper()
	if os.Getenv("TIFA_SSR_SOAK") == "" {
		t.Skip("set TIFA_SSR_SOAK=1 and build with -tags memory.counters")
	}
	return shopBundle(t)
}

// detail is the index of the order detail page in the pages of shopBundle, whose customer
// withCustomer renames.
const detail = 2

// nativeBaseline returns the live native allocations after one VM has been
// created and closed, which warms the allocator like the prototype measurements.
func nativeBaseline(t *testing.T) int {
	t.Helper()
	vm, err := slowjs.NewVM()
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

// Two VMs, two callers, 10,000 renders. Every render must carry its own data, and closing the
// pool returns native memory to the baseline. A render that fails would retire its VM, so no
// render may fail.
func TestSoakReuse(t *testing.T) {
	source, pages := soakBundle(t)
	baseline := nativeBaseline(t)
	cfg := shopConfig(2)
	p, err := ssr.NewPool(source, nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	const requests, callers = 10_000, 2
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			for i := range jobs {
				page := pages[i%len(pages)]
				in := page.input
				// Each request for the order detail page carries its own marker as the customer.
				marker, previous := "", ""
				if i%len(pages) == detail {
					marker = fmt.Sprintf("request-%05d-only", i)
					previous = fmt.Sprintf("request-%05d-only", i-len(pages))
					in = withCustomer(in, marker)
				}
				html, err := p.Render(context.Background(), in)
				if err != nil {
					t.Errorf("request %d: %v", i, err)
					continue
				}
				if !strings.Contains(html, marker) {
					t.Errorf("request %d: missing its own data", i)
				}
				if i >= len(pages) && previous != "" && strings.Contains(html, previous) {
					t.Errorf("request %d: data of an earlier request leaked into HTML", i)
				}
				if !strings.Contains(html, page.want) || strings.Contains(html, page.forbidden) {
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
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if after := libc.MemStat().Allocs; after != baseline {
		t.Fatalf("native allocations after close: baseline %d, after %d", baseline, after)
	}
	t.Logf("native allocations back to baseline %d", baseline)
}

// Fill a VM within one render, so idle replacement cannot preempt the OOM.
// Repeatedly parse the order detail input and the JSON text of its data, which contains Chinese
// text. Recovery must succeed eight times, and Close must return native memory to baseline.
func TestSoakOOMRecycle(t *testing.T) {
	source, pages := soakBundle(t)
	const retain = `;globalThis.startRender = (start => (json, ...texts) => {
 if (JSON.parse(json).preload === "<!-- oom -->") {
  const retained = [];
  for (;;) { retained.push(new Uint8Array(256 * 1024).fill(1)); JSON.parse(json); for (const text of texts) JSON.parse(text); }
 }
 return start(json, ...texts);
})(globalThis.startRender);`
	baseline := nativeBaseline(t)
	cfg := shopConfig(1)
	cfg.VMMemory = 32 << 20
	p, err := ssr.NewPool(source+retain, nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	page := pages[detail]
	for cycle := range 8 {
		failing := page.input
		failing.Preload = "<!-- oom -->"
		if _, err := p.Render(context.Background(), failing); !errors.Is(err, ssr.ErrOutOfMemory) {
			t.Fatalf("cycle %d: want ErrOutOfMemory, got %v", cycle, err)
		}
		if _, err := p.Render(context.Background(), page.input); err != nil {
			t.Fatalf("cycle %d: render after OOM: %v", cycle, err)
		}
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if after := libc.MemStat().Allocs; after != baseline {
		t.Fatalf("OOM recycling retained native allocations: baseline %d, after %d", baseline, after)
	}
	t.Logf("native allocations back to baseline %d", baseline)
}

// What a VM of the shop holds after a GC, against a VM loaded from an empty bundle, as
// TestSoakVMMemory in internal/ssr measures its testdata bundle: after loading, after formatting
// a number and a date, and after a render. It gives two counts of QuickJS: malloc, the bytes the
// VM's allocations take, which ssr.vm_memory limits, and used, its own estimate of what its
// objects, strings and functions take.
func TestSoakVMMemory(t *testing.T) {
	source, pages := soakBundle(t)
	const formats = `[new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY'}).format(1234.5),
		new Date(0).toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'})].join()`
	cfg := shopConfig(1)
	memory, stack := uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth)
	// measure returns what a VM from open holds, after use when it is set.
	measure := func(open func() (*slowjs.VM, error), use func(*slowjs.VM)) lib.TJSMemoryUsage {
		vm, err := open()
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()
		if use != nil {
			use(vm)
		}
		return ssr.VMMemory(vm, true)
	}
	loaded := func(source string) func() (*slowjs.VM, error) {
		bytecode, err := ssr.Compile(source, memory, cfg.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { bytecode.Close() })
		return func() (*slowjs.VM, error) { return ssr.Load(bytecode, memory, stack, cfg.Timeout, false) }
	}
	format := func(vm *slowjs.VM) {
		if _, err := vm.Eval(formats, slowjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
	}
	render := func(vm *slowjs.VM) {
		in, err := json.Marshal(pages[0].input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ssr.RenderVM(vm, in, pages[0].input.Texts, cfg.Timeout); err != nil {
			t.Fatal(err)
		}
	}
	kb := func(n int64) float64 { return float64(n) / 1024 }
	show := func(name string, u, base lib.TJSMemoryUsage) {
		t.Logf("%-24s malloc %8.1f KB (%+8.1f), used %8.1f KB (%+8.1f)", name,
			kb(u.Fmalloc_size), kb(u.Fmalloc_size-base.Fmalloc_size),
			kb(u.Fmemory_used_size), kb(u.Fmemory_used_size-base.Fmemory_used_size))
	}
	empty := measure(loaded(""), nil)
	open := loaded(source)
	show("empty bundle", empty, empty)
	show("loaded", measure(open, nil), empty)
	show("loaded, after formatting", measure(open, format), empty)
	show("loaded, after a render", measure(open, render), empty)
}
