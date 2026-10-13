package ssr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"modernc.org/libc"
	"modernc.org/quickjs"
)

// Diagnostic ablations keep the same compiled application and change one SSR
// phase at a time. These are deliberately excluded from the default test run.
func TestSSRMemoryProbes(t *testing.T) {
	if os.Getenv("SSR_MEMORY_PROBES") == "" {
		t.Skip("opt-in allocation diagnosis")
	}
	b, snapshot := fixture(t, "/projects/studio/orders/SH-1042")
	b.code = experimentSource(t, b.code)
	render := `const html = (0, import_server.renderToString)(/* @__PURE__ */ (0, import_jsx_runtime22.jsx)(RouterProvider, { router }));`
	if strings.Count(b.code, render) != 1 {
		t.Fatal("phase-rewriting probes require the archived unminified bundle via SSR_EXPERIMENT_SOURCE, use TestSSRStageProfile for current builds")
	}
	noReact := strings.Replace(b.code, render, `const html = "probe";`, 1)
	noDehydrate := strings.Replace(noReact, `await router.serverSsr.dehydrate();`, "", 1)
	createOnly := strings.Replace(noDehydrate, `await router.load();`, "", 1)
	for _, probe := range []struct{ name, code string }{
		{"minimal", counterProgram},
		{"weakmap_plain", `const cache = new WeakMap(); globalThis.startRender = () => { const key = {}; cache.set(key, {bytes: new Uint8Array(1024)}); globalThis.renderResult = '{"html":"probe"}'; };`},
		{"weakmap_cycle", `const cache = new WeakMap(); globalThis.startRender = () => { const key = {}; cache.set(key, {facade: key, bytes: new Uint8Array(1024)}); globalThis.renderResult = '{"html":"probe"}'; };`},
		{"polyfill_url", b.code + `; globalThis.startRender = () => { new URL('/projects/studio', 'http://go-quick.invalid'); globalThis.renderResult = '{"html":"probe"}'; };`},
		{"polyfill_abort", b.code + `; globalThis.startRender = () => { new AbortController(); globalThis.renderResult = '{"html":"probe"}'; };`},
		{"create_only", createOnly},
		{"load_only", noDehydrate},
		{"load_and_dehydrate", noReact},
		{"complete", b.code},
		{"complete_property_state", experimentPropertyState(t, b.code)},
	} {
		if selected := os.Getenv("SSR_PROBE"); selected != "" && !strings.Contains(","+selected+",", ","+probe.name+",") {
			continue
		}
		r := newTestExperiment(t, probe.code, experimentConfig{Mode: "reuse", Workers: 1, CollectEvery: 1, Timeout: 3 * time.Second})
		if libc.MemStat().Allocs == 0 {
			t.Fatal("use -tags memory.counters")
		}
		for batch := range 5 {
			for range 200 {
				if _, err := r.Render(context.Background(), snapshot); err != nil {
					t.Fatal(err)
				}
			}
			machine := <-r.ready
			for range 3 {
				machine.vm.SetGCThreshold(0)
				if _, err := machine.vm.Eval("void ({})", quickjs.EvalGlobal); err != nil {
					t.Fatal(err)
				}
				if _, err := machine.vm.ExecutePendingJobs(); err != nil {
					t.Fatal(err)
				}
			}
			r.ready <- machine
			if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"probe": probe.name, "requests": (batch + 1) * 200, "memory": experimentMem(t)}); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Diagnostic only: select core-js's existing property-backed fallback instead
// of its WeakMap-backed metadata. Do not rewrite the shipping bundle this way.
func experimentPropertyState(t *testing.T, code string) string {
	t.Helper()
	marker := `var NATIVE_WEAK_MAP = require_weak_map_basic_detection();`
	if strings.Count(code, marker) != 1 {
		t.Fatal("property-state ablation requires the archived unminified bundle via SSR_EXPERIMENT_SOURCE")
	}
	return strings.Replace(code, marker, `var NATIVE_WEAK_MAP = false;`, 1)
}

func experimentSource(t *testing.T, current string) string {
	t.Helper()
	if filename := os.Getenv("SSR_EXPERIMENT_SOURCE"); filename != "" {
		code, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		return string(code)
	}
	return current
}

func TestSSROOMMemoryProbe(t *testing.T) {
	if os.Getenv("SSR_MEMORY_PROBES") == "" {
		t.Skip("opt-in OOM allocation diagnosis")
	}
	program := "new Uint8Array(128 * 1024 * 1024)"
	if os.Getenv("SSR_OOM_FILL") != "" {
		program = "const retained = []; while (true) retained.push(new Uint8Array(256 * 1024));"
	}
	for i := range 100 {
		vm, err := quickjs.NewVM()
		if err != nil {
			t.Fatal(err)
		}
		vm.SetMemoryLimit(2 << 20)
		if _, err := vm.Eval(program, quickjs.EvalGlobal); err == nil {
			t.Fatal("expected OOM")
		}
		if err := vm.Close(); err != nil {
			t.Fatal(err)
		}
		if i == 0 || (i+1)%20 == 0 {
			if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"probe": "oom", "closed": i + 1, "memory": experimentMem(t)}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSSROOMReactProbe(t *testing.T) {
	if os.Getenv("SSR_MEMORY_PROBES") == "" {
		t.Skip("opt-in OOM allocation diagnosis")
	}
	b, snapshot := fixture(t, "/projects/studio/orders/SH-1042")
	b.code = experimentSource(t, b.code)
	if os.Getenv("SSR_OOM_PROPERTY_STATE") != "" {
		b.code = experimentPropertyState(t, b.code)
	}
	warm, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	if err := warm.Close(); err != nil {
		t.Fatal(err)
	}
	baseline := libc.MemStat().Allocs
	audit := os.Getenv("SSR_OOM_AUDIT") != ""
	if baseline == 0 && !audit {
		t.Fatal("use -tags memory.counters")
	}
	if audit {
		libc.MemAuditStart()
	}
	code := b.code + `; const originalStart = globalThis.startRender; const retained = []; globalThis.startRender = input => { const bytes = new Uint8Array(256 * 1024); bytes.fill(1); retained.push(bytes); originalStart(input); };`
	for cycle := range experimentEnvInt(t, "OOM_CYCLES", 8) {
		// End each cleanup scope before the next cycle, so the test itself
		// does not retain every renderer and its compiled Go byte slice.
		t.Run(fmt.Sprintf("cycle-%d", cycle+1), func(t *testing.T) {
			r := newTestExperiment(t, code, experimentConfig{Mode: "reuse", Workers: 1, MemoryLimit: 32 << 20, Timeout: 3 * time.Second})
			failed := false
			for i := range 128 {
				if _, err := r.Render(context.Background(), snapshot); err != nil {
					if !strings.Contains(err.Error(), "out of memory") {
						t.Fatal(err)
					}
					t.Logf("cycle %d failed request %d: %v", cycle, i, err)
					failed = true
					break
				}
			}
			if !failed {
				t.Fatal("expected OOM")
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"probe": "react_oom", "cycle": cycle + 1, "stats": r.stats(), "memory": experimentMem(t)}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if audit {
		if err := libc.MemAuditReport(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if after := libc.MemStat().Allocs; after != baseline {
		t.Fatalf("OOM cleanup retained native allocations: baseline %d, after %d", baseline, after)
	}
}

// Reduce the OOM failure independently of React, routing and URL polyfills.
func TestSSROOMArgumentProbe(t *testing.T) {
	if os.Getenv("SSR_MEMORY_PROBES") == "" {
		t.Skip("opt-in OOM argument diagnosis")
	}
	for _, body := range []string{
		`const retained = []; globalThis.probe = input => { const bytes = new Uint8Array(256 * 1024); bytes.fill(1); retained.push(bytes); return input.length; };`,
		`const retained = []; globalThis.probe = input => { const bytes = new Uint8Array(256 * 1024); bytes.fill(1); retained.push(bytes); return (() => input.length)(); };`,
	} {
		vm, err := quickjs.NewVM()
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.Close(); err != nil {
			t.Fatal(err)
		}
		baseline := libc.MemStat().Allocs
		if baseline == 0 {
			t.Fatal("use -tags memory.counters")
		}
		vm, err = quickjs.NewVM()
		if err != nil {
			t.Fatal(err)
		}
		vm.SetMemoryLimit((2 << 20) + (64 << 10))
		if _, err := vm.Eval(body, quickjs.EvalGlobal); err != nil {
			_ = vm.Close()
			t.Fatal(err)
		}
		function, err := vm.EvalValue("probe", quickjs.EvalGlobal)
		if err != nil {
			_ = vm.Close()
			t.Fatal(err)
		}
		this, err := vm.EvalValue("globalThis", quickjs.EvalGlobal)
		if err != nil {
			function.Free()
			_ = vm.Close()
			t.Fatal(err)
		}
		var callErr error
		for i := range 32 {
			if _, callErr = function.Call(this, strings.Repeat("测试数据", 250)); callErr != nil {
				t.Logf("call %d failed: %v", i, callErr)
				break
			}
		}
		function.Free()
		this.Free()
		if err := vm.Close(); err != nil {
			t.Fatal(err)
		}
		if callErr == nil || !strings.Contains(callErr.Error(), "out of memory") {
			t.Fatalf("expected OOM, got %v", callErr)
		}
		if after := libc.MemStat().Allocs; after != baseline {
			t.Errorf("argument OOM cleanup: baseline %d, after %d, program %s", baseline, after, body)
		}
	}
}
