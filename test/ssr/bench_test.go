package ssr_test

// Opt-in baseline of the real React bundle of examples/shop, for comparing SSR changes. Build the
// shop first (see shop_test.go), then run:
//
//	TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^render' -benchtime 2000x ./test/ssr
//	TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^(compile|load|first)' -benchtime 50x ./test/ssr
//	TIFA_SSR_SHOP=1 go test -run ShopVMMemory -v ./test/ssr
//	TIFA_SSR_SHOP=1 go test -run '^$' -bench ShopParallel -benchtime 15s ./test/ssr
//
// render-<page> renders one of the comparison pages in a warm VM and reports the P50 and
// P99 of single renders. compile is the startup compile of the bundle to bytecode, load creates one
// VM from the bytecode and reports its memory, first-render is the first render of a fresh VM.
// TestShopVMMemory splits the memory of one VM by what is in it. BenchmarkShopParallel renders
// with several VMs at once, to catch what only shows on several cores.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/slowjs"
	"github.com/liustack/slowjs/intl"
	lib "github.com/liustack/slowjs/lib"
	"github.com/liustack/tifa/internal/ssr"
)

func BenchmarkShop(b *testing.B) {
	source, pages := shopBundle(b)
	cfg := shopConfig(1)
	memory, timeout := uintptr(cfg.VMMemory), cfg.Timeout

	b.Run("compile", func(b *testing.B) {
		for b.Loop() {
			if _, err := ssr.Compile(source, memory, timeout); err != nil {
				b.Fatal(err)
			}
		}
	})

	bytecode, err := ssr.Compile(source, memory, timeout)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("load", func(b *testing.B) {
		var size int64
		for b.Loop() {
			vm, err := ssr.Load(bytecode, memory, uintptr(cfg.VMStackDepth), timeout, false)
			if err != nil {
				b.Fatal(err)
			}
			size = ssr.VMMemory(vm, false).Fmalloc_size
			if err := vm.Close(); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(size)/1024, "vm-KiB")
	})

	// The first render of a fresh VM, which a replaced VM pays.
	b.Run("first-render", func(b *testing.B) {
		page := pages[2]
		input := renderInput(b, page.input)
		for b.Loop() {
			b.StopTimer()
			vm, err := ssr.Load(bytecode, memory, uintptr(cfg.VMStackDepth), timeout, false)
			if err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			html, err := ssr.RenderVM(vm, input, timeout)
			b.StopTimer()
			if err != nil {
				b.Fatal(err)
			}
			page.check(b, html)
			if err := vm.Close(); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
		}
	})

	for _, page := range pages {
		b.Run("render"+strings.ReplaceAll(page.input.URL, "/", "-"), func(b *testing.B) {
			p, err := ssr.NewPool(source, nil, cfg, false)
			if err != nil {
				b.Fatal(err)
			}
			defer p.Close()
			for range 256 {
				if _, err := p.Render(context.Background(), page.input); err != nil {
					b.Fatal(err)
				}
			}
			var latencies []time.Duration
			for b.Loop() {
				begin := time.Now()
				html, err := p.Render(context.Background(), page.input)
				latencies = append(latencies, time.Since(begin))
				if err != nil {
					b.Fatal(err)
				}
				page.check(b, html)
			}
			slices.Sort(latencies)
			at := func(q float64) float64 {
				return float64(latencies[min(len(latencies)-1, int(q*float64(len(latencies))))]) / 1e6
			}
			b.ReportMetric(at(.50), "p50-ms")
			b.ReportMetric(at(.99), "p99-ms")
		})
	}
}

// BenchmarkShopParallel renders the six comparison pages in turn with a pool of 1 VM, then of
// GOMAXPROCS VMs, with one goroutine per VM so that every VM is busy and none waits for another.
// vms-<n> reports renders per second, the share of the rendering goroutines' time spent blocked
// on a lock, and for more than one VM, how many times the renders per second of vms-1 it reaches
// (only when vms-1 ran in the same command). A lock all VMs share, such as the allocator of
// modernc.org/libc, shows here as a speedup well below the VM count and a larger lock wait, while
// BenchmarkShop, which renders with one VM, does not change.
func BenchmarkShopParallel(b *testing.B) {
	source, pages := shopBundle(b)
	counts := []int{1}
	if n := runtime.GOMAXPROCS(0); n > 1 {
		counts = append(counts, n)
	}
	var single float64 // renders per second of vms-1, once it has run
	for _, n := range counts {
		var p *ssr.Pool
		b.Run(fmt.Sprintf("vms-%d", n), func(b *testing.B) {
			if p == nil {
				var err error
				if p, err = ssr.NewPool(source, nil, shopConfig(n), false); err != nil {
					b.Fatal(err)
				}
				if err := renderParallel(p, pages, n, 64*n); err != nil {
					b.Fatal(err)
				}
				b.ResetTimer()
			}
			waited := lockWait()
			begin := time.Now()
			if err := renderParallel(p, pages, n, b.N); err != nil {
				b.Fatal(err)
			}
			elapsed := time.Since(begin).Seconds()
			waited = lockWait() - waited
			rate := float64(b.N) / elapsed
			b.ReportMetric(rate, "renders/s")
			b.ReportMetric(100*waited/(float64(n)*elapsed), "lock-wait-%")
			if n == 1 {
				single = rate
			} else if single > 0 {
				b.ReportMetric(rate/single, "speedup")
			}
		})
		if p != nil {
			if err := p.Close(); err != nil {
				b.Error(err)
			}
		}
	}
}

// renderParallel makes count renders of the pages in turn with n goroutines, n being the VM count
// of p.
func renderParallel(p *ssr.Pool, pages []shopPage, n, count int) error {
	var next atomic.Int64
	errs := make([]error, n)
	var wg sync.WaitGroup
	for w := range n {
		wg.Go(func() {
			for i := int(next.Add(1)) - 1; i < count; i = int(next.Add(1)) - 1 {
				page := pages[i%len(pages)]
				html, err := p.Render(context.Background(), page.input)
				if err == nil && !page.matches(html) {
					err = fmt.Errorf("wrong HTML for %s", page.input.URL)
				}
				if err != nil {
					errs[w] = err
					return
				}
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// lockWait returns the seconds goroutines of this process have spent blocked on a sync.Mutex, a
// sync.RWMutex or a lock of the runtime so far. The runtime keeps this count all the time, so
// reading it changes nothing about the renders.
func lockWait() float64 {
	sample := []metrics.Sample{{Name: "/sync/mutex/wait/total:seconds"}}
	metrics.Read(sample)
	return sample[0].Value.Float64()
}

func renderInput(t testing.TB, in ssr.Input) []byte {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestShopVMMemory reports what one VM of the shop holds: the built-in objects of a bare VM, what
// Intl and then the host polyfills add, what the rest of the bundle adds, and what renders leave
// behind.
func TestShopVMMemory(t *testing.T) {
	source, pages := shopBundle(t)
	host, err := os.ReadFile(filepath.Join("..", "..", "js", "runtime", "host.js"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := shopConfig(1)
	bytecode, err := ssr.Compile(source, uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}

	bare, err := slowjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	builtins := ssr.VMMemory(bare, true)

	withIntl, err := slowjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer withIntl.Close()
	if err := intl.Register(withIntl); err != nil {
		t.Fatal(err)
	}
	intlAdded := ssr.VMMemory(withIntl, true)
	if _, err := withIntl.Eval(string(host), slowjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	hosted := ssr.VMMemory(withIntl, true)

	vm, err := ssr.Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	loaded := ssr.VMMemory(vm, true)
	for i := range 64 {
		page := pages[i%len(pages)]
		html, err := ssr.RenderVM(vm, renderInput(t, page.input), cfg.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
	}
	used := ssr.VMMemory(vm, false)
	collected := ssr.VMMemory(vm, true)

	t.Logf("bytecode of the bundle: %d bytes (source %d bytes)", len(bytecode), len(source))
	columns := []struct {
		name string
		m    lib.TJSMemoryUsage
	}{{"bare VM", builtins}, {"+ Intl", intlAdded}, {"+ host.js", hosted}, {"loaded bundle", loaded}, {"64 renders", used}, {"after GC", collected}}
	var out strings.Builder
	fmt.Fprintf(&out, "%-22s", "")
	for _, c := range columns {
		fmt.Fprintf(&out, "%16s", c.name)
	}
	out.WriteString("\n")
	for _, row := range memoryRows {
		fmt.Fprintf(&out, "%-22s", row.name)
		for _, c := range columns {
			fmt.Fprintf(&out, "%16d", row.value(&c.m))
		}
		out.WriteString("\n")
	}
	t.Logf("memory of one VM (sizes in bytes, from JS_ComputeMemoryUsage):\n%s", out.String())
}

var memoryRows = []struct {
	name  string
	value func(*lib.TJSMemoryUsage) int64
}{
	{"malloc size", func(m *lib.TJSMemoryUsage) int64 { return m.Fmalloc_size }},
	{"malloc count", func(m *lib.TJSMemoryUsage) int64 { return m.Fmalloc_count }},
	{"memory used size", func(m *lib.TJSMemoryUsage) int64 { return m.Fmemory_used_size }},
	{"atoms", func(m *lib.TJSMemoryUsage) int64 { return m.Fatom_count }},
	{"atom size", func(m *lib.TJSMemoryUsage) int64 { return m.Fatom_size }},
	{"strings", func(m *lib.TJSMemoryUsage) int64 { return m.Fstr_count }},
	{"string size", func(m *lib.TJSMemoryUsage) int64 { return m.Fstr_size }},
	{"objects", func(m *lib.TJSMemoryUsage) int64 { return m.Fobj_count }},
	{"object size", func(m *lib.TJSMemoryUsage) int64 { return m.Fobj_size }},
	{"properties", func(m *lib.TJSMemoryUsage) int64 { return m.Fprop_count }},
	{"property size", func(m *lib.TJSMemoryUsage) int64 { return m.Fprop_size }},
	{"shapes", func(m *lib.TJSMemoryUsage) int64 { return m.Fshape_count }},
	{"shape size", func(m *lib.TJSMemoryUsage) int64 { return m.Fshape_size }},
	{"JS functions", func(m *lib.TJSMemoryUsage) int64 { return m.Fjs_func_count }},
	{"JS function size", func(m *lib.TJSMemoryUsage) int64 { return m.Fjs_func_size }},
	{"bytecode size", func(m *lib.TJSMemoryUsage) int64 { return m.Fjs_func_code_size }},
	{"pc2line size", func(m *lib.TJSMemoryUsage) int64 { return m.Fjs_func_pc2line_size }},
	{"C functions", func(m *lib.TJSMemoryUsage) int64 { return m.Fc_func_count }},
	{"arrays", func(m *lib.TJSMemoryUsage) int64 { return m.Farray_count }},
	{"fast array elements", func(m *lib.TJSMemoryUsage) int64 { return m.Ffast_array_elements }},
	{"binary object size", func(m *lib.TJSMemoryUsage) int64 { return m.Fbinary_object_size }},
}
