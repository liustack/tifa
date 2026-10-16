package ssr

// Opt-in baseline of the real React bundle of examples/shop, for comparing SSR changes. Build the
// shop first (see shop_test.go), then run:
//
//	TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^render' -benchtime 2000x ./internal/ssr
//	TIFA_SSR_SHOP=1 go test -run '^$' -bench 'Shop/^(compile|load|first)' -benchtime 50x ./internal/ssr
//	TIFA_SSR_SHOP=1 go test -run ShopVMMemory -v ./internal/ssr
//
// render-<page> renders one of the comparison pages in a warm VM and reports the P50 and
// P99 of single renders. compile is the startup compile of the bundle to bytecode, load creates one
// VM from the bytecode and reports its memory, first-render is the first render of a fresh VM.
// TestShopVMMemory splits the memory of one VM by what is in it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/liustack/quickjs"
	"github.com/liustack/quickjs/intl"
	lib "github.com/liustack/quickjs/lib"
	"modernc.org/libc"
)

func BenchmarkShop(b *testing.B) {
	source, pages := shopBundle(b)
	cfg := shopConfig(1)
	memory, timeout := uintptr(cfg.VMMemory), cfg.Timeout

	b.Run("compile", func(b *testing.B) {
		for b.Loop() {
			if _, err := compile(source, memory, timeout); err != nil {
				b.Fatal(err)
			}
		}
	})

	bytecode, err := compile(source, memory, timeout)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("load", func(b *testing.B) {
		var size int64
		for b.Loop() {
			vm, err := load(bytecode, memory, timeout, false)
			if err != nil {
				b.Fatal(err)
			}
			size = vmMemory(vm, false).Fmalloc_size
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
			vm, err := load(bytecode, memory, timeout, false)
			if err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			html, err := render(vm, input, timeout)
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
			p, err := NewPool(source, nil, cfg, false)
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

func renderInput(t testing.TB, in Input) []byte {
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
	bytecode, err := compile(source, uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}

	bare, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	builtins := vmMemory(bare, true)

	withIntl, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer withIntl.Close()
	if err := intl.Register(withIntl); err != nil {
		t.Fatal(err)
	}
	intlAdded := vmMemory(withIntl, true)
	if _, err := withIntl.Eval(string(host), quickjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	hosted := vmMemory(withIntl, true)

	vm, err := load(bytecode, uintptr(cfg.VMMemory), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	loaded := vmMemory(vm, true)
	for i := range 64 {
		page := pages[i%len(pages)]
		html, err := render(vm, renderInput(t, page.input), cfg.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
	}
	used := vmMemory(vm, false)
	collected := vmMemory(vm, true)

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
