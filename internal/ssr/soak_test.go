package ssr

// Opt-in long tests of VM reuse and OOM recycling. They count QuickJS native
// allocations, so they need the memory.counters build tag:
//
//	TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/liustack/quickjs"
	lib "github.com/liustack/quickjs/lib"
	"github.com/liustack/tifa/internal/config"
	"modernc.org/libc"
)

func skipUnlessSoak(t *testing.T) {
	t.Helper()
	if os.Getenv("TIFA_SSR_SOAK") == "" {
		t.Skip("set TIFA_SSR_SOAK=1 and build with -tags memory.counters")
	}
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
	skipUnlessSoak(t)
	baseline := nativeBaseline(t)
	cfg := config.SSR{VMs: 2, VMMemory: 128 << 20, VMStackDepth: config.Default().SSR.VMStackDepth, Timeout: 10 * time.Second}
	p, err := NewPool(testSource(t), nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	const requests, callers = 10_000, 2
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			for i := range jobs {
				// Every request carries its own marker, which the page echoes.
				marker := fmt.Sprintf("request-%05d-only", i)
				previous := fmt.Sprintf("request-%05d-only", i-1)
				html, err := p.Render(context.Background(), input("garbage", marker))
				if err != nil {
					t.Errorf("request %d: %v", i, err)
					continue
				}
				if !strings.Contains(html, marker) {
					t.Errorf("request %d: missing its own data", i)
				}
				if i > 0 && strings.Contains(html, previous) {
					t.Errorf("request %d: data of an earlier request leaked into HTML", i)
				}
			}
		})
	}
	for i := range requests {
		jobs <- i
		if (i+1)%2_000 == 0 {
			t.Logf("%d renders, native allocations %d", i+1, libc.MemStat().Allocs)
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
}

// Each render keeps 256 KiB alive until the VM hits its 32 MiB limit. The pool
// must replace the VM, serve the next render from a fresh one, and after eight
// such cycles and Close, native memory must be back at the baseline.
func TestSoakOOMRecycle(t *testing.T) {
	skipUnlessSoak(t)
	const retain = `;globalThis.startRender = (start => { const retained = []; return json => { retained.push(new Uint8Array(256 * 1024).fill(1)); start(json) } })(globalThis.startRender);`
	baseline := nativeBaseline(t)
	p, err := NewPool(testSource(t)+retain, nil, config.SSR{VMs: 1, VMMemory: 32 << 20, VMStackDepth: config.Default().SSR.VMStackDepth, Timeout: 10 * time.Second}, false)
	if err != nil {
		t.Fatal(err)
	}
	page := input("ok", "")
	for cycle := range 8 {
		before := p.stats()
		failed := false
		for i := range 256 {
			_, err := p.Render(context.Background(), page)
			if err == nil {
				continue
			}
			// Only the memory limit may end a cycle. A timeout or a page error fails
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
		if _, err := p.Render(context.Background(), page); err != nil {
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
}

// What a VM holds after a GC. Every VM here but the bare one comes from Load, as in the pool, since
// a bundle may use Intl at its top level. An empty bundle shows what Intl adds: compared with a bare
// VM, the stand-ins of a VM whose pages never format, and what formatting a number and a date
// installs. The testdata bundle then shows what it adds after loading, formatting and a render. It
// gives two counts of QuickJS: malloc, the bytes the VM's allocations take, which ssr.vm_memory
// limits, and used, its own estimate of what its objects, strings and functions take.
func TestSoakVMMemory(t *testing.T) {
	skipUnlessSoak(t)
	const formats = `[new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY'}).format(1234.5),
		new Date(0).toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'})].join()`
	const memory, timeout = 128 << 20, 10 * time.Second
	stack := uintptr(config.Default().SSR.VMStackDepth)
	// measure returns what a VM from open holds, after use when it is set.
	measure := func(t *testing.T, open func() (*quickjs.VM, error), use func(*quickjs.VM)) lib.TJSMemoryUsage {
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
	loaded := func(t *testing.T, source string) func() (*quickjs.VM, error) {
		bytecode, err := Compile(source, memory, timeout)
		if err != nil {
			t.Fatal(err)
		}
		return func() (*quickjs.VM, error) { return Load(bytecode, memory, stack, timeout, false) }
	}
	format := func(vm *quickjs.VM) {
		if _, err := vm.Eval(formats, quickjs.EvalGlobal); err != nil {
			t.Fatal(err)
		}
	}
	kb := func(n int64) float64 { return float64(n) / 1024 }
	show := func(t *testing.T, name string, u, base lib.TJSMemoryUsage) {
		t.Logf("%-24s malloc %8.1f KB (%+8.1f), used %8.1f KB (%+8.1f)", name,
			kb(u.Fmalloc_size), kb(u.Fmalloc_size-base.Fmalloc_size),
			kb(u.Fmemory_used_size), kb(u.Fmemory_used_size-base.Fmemory_used_size))
	}

	bare := measure(t, quickjs.NewVM, nil)
	empty := loaded(t, "")
	unused := measure(t, empty, nil)
	show(t, "bare VM", bare, bare)
	show(t, "Intl, unused", unused, bare)
	show(t, "Intl, after formatting", measure(t, empty, format), bare)
	if grown := unused.Fmalloc_size - bare.Fmalloc_size; grown > 32<<10 {
		t.Fatalf("Intl adds %.1f KB to a VM that never formats", kb(grown))
	}

	open := loaded(t, testSource(t))
	render := func(vm *quickjs.VM) {
		in, err := json.Marshal(input("ok", ""))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := RenderVM(vm, in, timeout); err != nil {
			t.Fatal(err)
		}
	}
	show(t, "loaded", measure(t, open, nil), unused)
	show(t, "loaded, after formatting", measure(t, open, format), unused)
	show(t, "loaded, after a render", measure(t, open, render), unused)
}

// vmMemory returns what QuickJS counts in the runtime of vm, after a GC when collect is set.
// Each VM of the pool has a runtime of its own. The quickjs package does not export the runtime,
// so the test reads it from the VM, as the fork's own tests do.
func vmMemory(vm *quickjs.VM, collect bool) lib.TJSMemoryUsage {
	rt := (*struct {
		c   uintptr
		tls *libc.TLS
	})(reflect.ValueOf(vm).Elem().FieldByName("runtime").UnsafePointer())
	if collect {
		lib.XJS_RunGC(rt.tls, rt.c)
	}
	n := int(unsafe.Sizeof(lib.TJSMemoryUsage{}))
	p := rt.tls.Alloc(n)
	defer rt.tls.Free(n)
	lib.XJS_ComputeMemoryUsage(rt.tls, rt.c, p)
	// p is C memory of the VM's allocator, which the Go GC neither moves nor frees.
	return *(*lib.TJSMemoryUsage)(unsafe.Add(nil, p))
}
