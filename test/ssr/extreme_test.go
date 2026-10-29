package ssr_test

// The extreme scenarios of test/apps/extreme, each at the sizes of its table in docs/testing.md.
// They skip unless TIFA_SSR_EXTREME=1. Build the app first, then run:
//
//	cd test/apps/extreme && pnpm install --frozen-lockfile && go tool tifa build
//	TIFA_SSR_EXTREME=1 go test -run '^$' -bench Extreme -benchtime 5x ./test/ssr
//	TIFA_SSR_EXTREME=1 go test -run ExtremeIsolation -v ./test/ssr
//
// BenchmarkExtreme renders each size with the default settings of a program (ssr.vm_memory
// 128 MiB, ssr.timeout 3 s, ssr.vm_stack_depth 10000). For a size that renders it reports the P50
// and P99 of warm renders, the VM's memory right after a render and before any GC between
// renders, and how much the goroutine stack of a render grew. For a size that fails it logs how,
// how soon, and what the render takes with 1 GiB and 60 seconds instead.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
)

const extremeDir = "../../test/apps/extreme"

// extremeTimeout is a page that renders for well over the one second TestExtremeIsolation gives
// a render, without running out of 128 MiB first.
const extremeTimeout = "/markdown/200"

// extremeSizes are the sizes of each scenario, with the route ID of its page and texts its HTML
// must have.
var extremeSizes = []struct{ url, route, want string }{
	{"/dom/10000", "app/dom/n+/route", `id="files"`},
	{"/dom/50000", "app/dom/n+/route", `id="files"`},
	{"/dom/100000", "app/dom/n+/route", `id="files"`},
	{"/nest/500", "app/nest/n+/route", `id="leaf"`},
	{"/nest/1000", "app/nest/n+/route", `id="leaf"`},
	{"/nest/5000", "app/nest/n+/route", `id="leaf"`},
	{"/nest/10000", "app/nest/n+/route", `id="leaf"`},
	{"/deep/400", "app/deep/n+/route", `id="bottom"`},
	{"/deep/600", "app/deep/n+/route", `id="bottom"`},
	{"/inject/1", "app/inject/mb+/route", `id="items"`},
	{"/inject/5", "app/inject/mb+/route", `id="items"`},
	{"/inject/20", "app/inject/mb+/route", `id="items"`},
	{"/json/1999", "app/json/n+/route", `id="levels">1998<`},
	{"/json/2001", "app/json/n+/route", `id="levels">2000<`},
	{"/table/1000", "app/table/rows+/route", `SH-200999`},
	{"/table/5000", "app/table/rows+/route", `SH-204999`},
	{"/table/10000", "app/table/rows+/route", `SH-209999`},
	{"/providers/200", "app/providers/n+/route", `id="sum">20100<`},
	{"/providers/500", "app/providers/n+/route", `id="sum">125250<`},
	{"/providers/1000", "app/providers/n+/route", `id="sum">500500<`},
	{"/charts/12", "app/charts/n+/route", `<circle`},
	{"/markdown/200", "app/markdown/kb+/route", `id="blocks">50<`},
	{"/markdown/1000", "app/markdown/kb+/route", `id="blocks">250<`},
	{"/markdown/2000", "app/markdown/kb+/route", `id="blocks">500<`},
	{"/i18n/3000", "app/i18n/keys+/route", `id="formats"`},
	{"/mall/zh-CN", "app/mall/lang+/route", `id="title"`},
	{"/mall/en-US", "app/mall/lang+/route", `id="title"`},
	{"/console/100", "app/console/rows+/route", `id="comments">300<`},
}

type extremePage struct {
	input  ssr.Input
	want   string
	answer []byte        // the answer of the data request, as the program wrote it
	fetch  time.Duration // how long the data request took, which runs the Load and writes its JSON
}

func (p extremePage) check(t testing.TB, html string) {
	t.Helper()
	if !strings.Contains(html, p.want) || !strings.Contains(html, "data-tsr-stream-part") {
		t.Fatalf("wrong HTML for %s: no %s", p.input.URL, p.want)
	}
}

// extremeApp returns the app's SSR bundle and a function that fetches the input of a page from
// the app's own program, as shopBundle does for the shop.
func extremeApp(t testing.TB) (source string, page func(url, route, want string) extremePage) {
	t.Helper()
	if os.Getenv("TIFA_SSR_EXTREME") == "" {
		t.Skip("set TIFA_SSR_EXTREME=1 after running tifa build in test/apps/extreme")
	}
	data, err := os.ReadFile(filepath.Join(extremeDir, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatalf("run tifa build in test/apps/extreme first: %v", err)
	}
	var built struct{ BuildID string }
	manifest, err := os.ReadFile(filepath.Join(extremeDir, ".tifa", "dist", "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifest, &built); err != nil {
		t.Fatal(err)
	}
	base := startProgram(t, filepath.Join(extremeDir, "bin", "extremetest"))
	return string(data), func(url, route, want string) extremePage {
		req, err := http.NewRequest("GET", base+url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Tifa-Route", route)
		req.Header.Set("X-Tifa-Build", built.BuildID)
		begin := time.Now()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		fetch := time.Since(begin)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("data of %s: %d %v %.200s (rebuild the app when its build.json changed)", url, res.StatusCode, err, body)
		}
		data, texts := injectedInput(t, body)
		return extremePage{ssr.Input{BuildID: built.BuildID, URL: url, Data: data, Texts: texts}, want, body, fetch}
	}
}

// extremeConfig is the default SSR settings of a program, with n VMs.
func extremeConfig(n int) config.SSR {
	cfg := config.Default().SSR
	cfg.VMs = n
	return cfg
}

// once is one render in a fresh VM, on a goroutine of its own.
type once struct {
	html  string
	err   error
	took  time.Duration
	vm    uintptr // the VM's memory right after the render, before any GC between renders
	stack uint64  // how much the goroutine stacks of the process grew during the render
}

// renderOnce loads a VM from bytecode with the limits of cfg and renders in it on a new
// goroutine, which starts with a small stack.
func renderOnce(t testing.TB, bytecode *slowjs.SharedBytecode, cfg config.SSR, in ssr.Input) once {
	t.Helper()
	vm, err := ssr.Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	input := renderInput(t, in)
	done := make(chan once)
	go func() {
		runtime.GC()
		before := goStacks()
		begin := time.Now()
		html, err := ssr.RenderVM(vm, input, in.Texts, cfg.Timeout)
		took := time.Since(begin)
		r := once{html: html, err: err, took: took, stack: goStacks() - before}
		// A render that failed leaves the VM good only for Close, as RenderVM says.
		if err == nil {
			r.vm = vm.MemorySize()
		}
		done <- r
	}()
	r := <-done
	// A VM whose render failed may only be closed, which is what the pool does with it.
	if err := vm.Close(); err != nil && r.err == nil {
		t.Fatal(err)
	}
	return r
}

// goStacks returns the bytes of the stacks of all goroutines. A stack grows by doubling and
// shrinks only during a GC, so after a render it still holds what the render grew it to.
func goStacks() uint64 {
	s := []metrics.Sample{{Name: "/memory/classes/heap/stacks:bytes"}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

// failure names how a render failed.
func failure(err error) string {
	var e *slowjs.Error
	switch {
	case errors.Is(err, ssr.ErrOutOfMemory):
		return "out of memory"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.As(err, &e):
		return "engine error: " + e.Error()
	}
	return err.Error()
}

func BenchmarkExtreme(b *testing.B) {
	source, page := extremeApp(b)
	cfg := extremeConfig(1)
	bytecode, err := ssr.Compile(source, uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		b.Fatal(err)
	}
	defer bytecode.Close()
	roomy := cfg
	roomy.VMMemory, roomy.Timeout = 1<<30, time.Minute
	for _, size := range extremeSizes {
		b.Run(strings.ReplaceAll(size.url[1:], "/", "-"), func(b *testing.B) {
			p := page(size.url, size.route, size.want)
			first := renderOnce(b, bytecode, cfg, p.input)
			if first.err != nil {
				msg := fmt.Sprintf("%s fails: %s after %.0f ms", size.url, failure(first.err), float64(first.took)/1e6)
				if r := renderOnce(b, bytecode, roomy, p.input); r.err == nil {
					msg += fmt.Sprintf(". With 1 GiB and 60 s it renders in %.0f ms, VM %.1f MiB after the render", float64(r.took)/1e6, float64(r.vm)/(1<<20))
				} else {
					msg += fmt.Sprintf(". With 1 GiB and 60 s it fails too: %s after %.0f ms", failure(r.err), float64(r.took)/1e6)
				}
				b.Log(msg)
				b.Skip(failure(first.err))
			}
			p.check(b, first.html)
			report := func() {}
			if strings.HasPrefix(size.url, "/inject/") {
				report = injection(b, p)
			}
			pool, err := ssr.NewPool(source, nil, cfg, false)
			if err != nil {
				b.Fatal(err)
			}
			defer pool.Close()
			for range 2 {
				if _, err := pool.Render(context.Background(), p.input); err != nil {
					b.Fatal(err)
				}
			}
			var latencies []time.Duration
			for b.Loop() {
				begin := time.Now()
				html, err := pool.Render(context.Background(), p.input)
				latencies = append(latencies, time.Since(begin))
				if err != nil {
					b.Fatal(err)
				}
				p.check(b, html)
			}
			slices.Sort(latencies)
			at := func(q float64) float64 {
				return float64(latencies[min(len(latencies)-1, int(q*float64(len(latencies))))]) / 1e6
			}
			b.ReportMetric(at(.50), "p50-ms")
			b.ReportMetric(at(.99), "p99-ms")
			b.ReportMetric(float64(first.vm)/(1<<20), "vm-MiB")
			b.ReportMetric(float64(first.stack)/(1<<10), "stack-KiB")
			b.ReportMetric(float64(len(first.html))/(1<<10), "html-KiB")
			report()
		})
	}
}

// injection measures what the parts of injecting the data of p cost apart from the render:
// the data request, which runs the Load and writes its JSON, escaping the characters above U+FFFF
// in that JSON, and parsing the text in a VM of its own, passing it in included. It returns a
// function that reports them, since b.Loop drops the metrics reported before it.
func injection(b *testing.B, p extremePage) (report func()) {
	b.Helper()
	begin := time.Now()
	escaped := ssr.EscapeAboveFFFF(p.answer)
	escape := time.Since(begin)
	vm, err := slowjs.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()
	vm.SetMemoryLimit(1 << 30)
	if _, err := vm.Eval("globalThis.parse = (text) => JSON.parse(text) && text.length", slowjs.EvalGlobal); err != nil {
		b.Fatal(err)
	}
	begin = time.Now()
	for _, text := range p.input.Texts {
		if _, err := vm.Call("parse", text); err != nil {
			b.Fatal(err)
		}
	}
	parse := time.Since(begin)
	return func() {
		b.ReportMetric(float64(len(escaped))/(1<<20), "data-MiB")
		b.ReportMetric(float64(p.fetch)/1e6, "load-ms")
		b.ReportMetric(float64(escape)/1e6, "escape-ms")
		b.ReportMetric(float64(parse)/1e6, "parse-ms")
	}
}

// TestExtremeIsolation renders a light page with three of the four VMs of a pool while the fourth
// renders a heavy page again and again: once where the heavy page renders, and once where it runs
// past the pool's time budget of a second each time, so that the pool stops its VM and loads a new
// one. Stopping and replacing VMs must not slow the light page down by much more than the heavy
// work alone does. Both runs load the CPU equally, so the comparison leaves out what the cores
// and the GC of the process cost anyway.
func TestExtremeIsolation(t *testing.T) {
	source, page := extremeApp(t)
	light := page("/table/25", "app/table/rows+/route", "SH-200024")
	heavy := page(extremeTimeout, "app/markdown/kb+/route", "")
	cfg := extremeConfig(4)
	cfg.Timeout = 10 * time.Second
	finishes := newPool(t, source, cfg)
	cfg.Timeout = time.Second
	stops := newPool(t, source, cfg)
	const each = 600
	run := func(p *ssr.Pool, busy bool) (p50, p99 float64, heavyRuns int) {
		for range 20 {
			if _, err := p.Render(context.Background(), light.input); err != nil {
				t.Fatal(err)
			}
		}
		var stop atomic.Bool
		var wg sync.WaitGroup
		if busy {
			wg.Go(func() {
				for !stop.Load() {
					begin := time.Now()
					_, err := p.Render(context.Background(), heavy.input)
					if p == stops && !errors.Is(err, context.DeadlineExceeded) || p == finishes && err != nil {
						t.Errorf("%s: %v after %s", extremeTimeout, err, time.Since(begin))
						return
					}
					heavyRuns++
				}
			})
			time.Sleep(200 * time.Millisecond) // the heavy render has started
		}
		latencies := make([][]float64, 3)
		var light3 sync.WaitGroup
		for g := range latencies {
			light3.Go(func() {
				for range each {
					begin := time.Now()
					html, err := p.Render(context.Background(), light.input)
					latencies[g] = append(latencies[g], float64(time.Since(begin))/1e6)
					if err != nil {
						t.Error(err)
						return
					}
					light.check(t, html)
				}
			})
		}
		light3.Wait()
		stop.Store(true)
		wg.Wait()
		all := slices.Concat(latencies...)
		slices.Sort(all)
		return all[len(all)/2], all[len(all)*99/100], heavyRuns
	}
	alone50, alone99, _ := run(stops, false)
	busy50, busy99, finished := run(finishes, true)
	stop50, stop99, stopped := run(stops, true)
	t.Logf("%s with 3 VMs (GOMAXPROCS %d): P50 %.2f ms, P99 %.2f ms alone; P50 %.2f ms, P99 %.2f ms while the 4th VM rendered %s %d times; P50 %.2f ms, P99 %.2f ms while it was stopped and replaced %d times",
		light.input.URL, runtime.GOMAXPROCS(0), alone50, alone99, busy50, busy99, extremeTimeout, finished, stop50, stop99, stopped)
	if runtime.GOMAXPROCS(0) < 4 {
		t.Skip("needs 4 CPUs for the 4 VMs to run at once, the numbers above are only for reading")
	}
	if stopped == 0 {
		t.Fatalf("%s never ran past the time budget", extremeTimeout)
	}
	if limit := max(1.5*busy99, busy99+5); stop99 > limit {
		t.Errorf("P99 of %s was %.2f ms while VMs were stopped and replaced, more than %.2f ms", light.input.URL, stop99, limit)
	}
}
