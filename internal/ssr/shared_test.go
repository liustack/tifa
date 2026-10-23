package ssr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// borrow runs f on an idle VM of p.
func borrow(t *testing.T, p *Pool, f func(*pooledVM)) {
	t.Helper()
	vm, err := p.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer p.putIdle(vm)
	f(vm)
}

// The source text and line tables of the bundle's functions are kept once for the pool, not in each
// VM: a function with a large source adds next to nothing to the memory of a VM, and its toString
// still gives the whole text.
func TestPoolSharesTheSource(t *testing.T) {
	const pad = 512 << 10
	fn := "function padded() { /*" + strings.Repeat("x", pad) + "*/ return 1 }"
	plain := newTestPool(t, testSource(t), testConfig(2))
	padded := newTestPool(t, testSource(t)+"\nglobalThis.padded = "+fn+";\n", testConfig(2))

	var small, large uintptr
	borrow(t, plain, func(vm *pooledVM) { small = vm.live })
	borrow(t, padded, func(vm *pooledVM) {
		large = vm.live
		got, err := vm.Eval("String(globalThis.padded)", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got != fn {
			t.Errorf("toString gives %d bytes, want the %d of the source", len(got.(string)), len(fn))
		}
	})
	if large < small || large-small > pad/8 {
		t.Fatalf("a VM of the bundle with %d bytes more source holds %d bytes, one without %d", pad, large, small)
	}
}

// A VM loaded from the bytecode of a pool keeps working after the pool is closed: the bytecode
// stays until the last VM that loaded it is closed.
func TestVMOutlivesThePool(t *testing.T) {
	cfg := testConfig(1)
	p, err := NewPool(testSource(t), nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	vm, err := Load(p.bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	in, err := json.Marshal(input("ok", "after close"))
	if err != nil {
		t.Fatal(err)
	}
	html, err := RenderVM(vm, in, cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "after close") {
		t.Fatalf("got %s", html)
	}
	if _, err := Load(p.bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false); err == nil {
		t.Fatal("loaded the bytecode of a closed pool")
	}
}
