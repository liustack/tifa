package ssr

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liustack/quickjs"
)

// The objects of the cycles and hold renders of testdata/app.js.
const (
	cyclesJS = "for (let i = 0; i < 10000; i++) { const node = { index: i }; node.self = node }; undefined"
	holdJS   = "globalThis.held = []; for (let i = 0; i < 10000; i++) held.push({ index: i }); undefined"
)

// objectSize returns the bytes each of the 10000 objects js makes takes, measured on a VM of the
// test bundle that does not collect, so that the tests size renders by the live size of the VM
// on any platform.
func objectSize(t *testing.T, js string) uintptr {
	t.Helper()
	cfg := testConfig(1)
	bytecode, err := Compile(testSource(t), uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	vm, err := Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	vm.SetGCThreshold(^uintptr(0))
	before := vm.MemorySize()
	if _, err := vm.Eval(js, quickjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	return (vm.MemorySize() - before) / 10000
}

// idleVM waits for the VM of a pool of one, also while it is collected between renders, and hands
// it to look. The VM goes back to the pool after.
func idleVM(p *Pool, look func(vm *pooledVM)) {
	vm := <-p.idle
	defer func() { p.idle <- vm }()
	look(vm)
}

// Each garbage render leaves cycles that only a GC frees. The pool collects them between renders,
// so no render collects, after each GC the VM holds about what it held after Load, and the
// threshold for the next render is twice that.
func TestPoolCollectsBetweenRenders(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	var loaded uintptr
	idleVM(p, func(vm *pooledVM) { loaded = vm.MemorySize() })
	for i := range 300 {
		html, err := p.Render(context.Background(), input("garbage", fmt.Sprint(i)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, fmt.Sprintf(">%d</main>", i)) {
			t.Fatalf("render %d: %s", i, html)
		}
		idleVM(p, func(vm *pooledVM) {
			if size := vm.MemorySize(); size >= loaded+loaded/2+loaded/8 {
				t.Fatalf("render %d: an idle VM holds %d bytes, %d after Load", i, size, loaded)
			}
			if vm.live > loaded+loaded/8 {
				t.Fatalf("render %d: a GC left %d bytes, %d after Load", i, vm.live, loaded)
			}
			if got := vm.GCThreshold(); got != 2*vm.live {
				t.Fatalf("render %d: GC threshold %d, want twice the live size %d", i, got, vm.live)
			}
		})
	}
	s := p.stats()
	if s.idleGCs < 10 {
		t.Fatalf("%d GCs between 300 renders of garbage", s.idleGCs)
	}
	if s.renderGCs != 0 {
		t.Fatalf("%d renders ran a GC", s.renderGCs)
	}
}

// Load collects what loading left, so the live size of a fresh VM is what its bundle keeps, and
// it sets the threshold of the first render.
func TestLoadCollects(t *testing.T) {
	cfg := testConfig(1)
	bytecode, err := Compile(testSource(t), uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	vm, err := Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	loaded := vm.MemorySize()
	vm.RunGC()
	if got := vm.MemorySize(); got != loaded {
		t.Fatalf("a GC after Load freed %d bytes", loaded-got)
	}
	if got := vm.GCThreshold(); got != 2*loaded {
		t.Fatalf("GC threshold %d after Load, want twice the live size %d", got, loaded)
	}
}

// With a memory limit under three times the live size, the threshold stays half way from the live
// size to the limit, so cycles are collected before they fill the VM.
func TestGCThresholdStaysUnderTheLimit(t *testing.T) {
	var live uintptr
	idleVM(newTestPool(t, testSource(t), testConfig(1)), func(vm *pooledVM) { live = vm.live })
	cfg := testConfig(1)
	cfg.VMMemory = int64(live * 5 / 2)
	p := newTestPool(t, testSource(t), cfg)
	for i := range 100 {
		if _, err := p.Render(context.Background(), input("garbage", "")); err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		idleVM(p, func(vm *pooledVM) {
			limit := vm.live + (p.memory-vm.live)/2
			if limit >= 2*vm.live {
				t.Fatalf("render %d: live size %d, limit %d leave room for twice the live size", i, vm.live, p.memory)
			}
			if got := vm.GCThreshold(); got > limit || got+1 < limit {
				t.Fatalf("render %d: GC threshold %d, want half way from the live size %d to the limit %d", i, got, vm.live, p.memory)
			}
		})
	}
	if s := p.stats(); s.created != 1 || s.idleGCs == 0 {
		t.Fatalf("want one VM and GCs between renders, got %+v", s)
	}
}

// A render that makes more garbage than the threshold leaves room for collects during the render.
// The engine then sets its own threshold, 1.5 times what its GC left, and the pool puts its own
// back before the next render, also when the render left too little garbage for a GC after it.
func TestGCThresholdIsResetAfterTheEnginesGC(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	var live uintptr
	idleVM(p, func(vm *pooledVM) { live = vm.live })
	// About 1.15 times the live size: the engine collects once, at twice the live size, and what is
	// left after the render stays under 1.5 times.
	count := fmt.Sprint(live * 23 / 20 / objectSize(t, cyclesJS))
	if _, err := p.Render(context.Background(), input("cycles", count)); err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.renderGCs != 1 || s.idleGCs != 0 {
		t.Fatalf("want one render that ran a GC and none after it, got %+v", s)
	}
	idleVM(p, func(vm *pooledVM) {
		if got := vm.GCThreshold(); got != 2*vm.live {
			t.Fatalf("GC threshold %d after the engine's GC, want twice the live size %d", got, vm.live)
		}
	})
}

// A render that holds more than the live size until it ends makes the engine collect at twice the
// live size, and the threshold the engine then sets, 1.5 times what its GC left, counts what the
// render held. The pool keeps that threshold for the next renders, which then do not collect.
func TestGCThresholdLeavesRoomForWhatARenderHolds(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	var live uintptr
	idleVM(p, func(vm *pooledVM) { live = vm.live })
	// The render holds about 1.5 times the live size.
	count := fmt.Sprint(live * 3 / 2 / objectSize(t, holdJS))
	for i := range 20 {
		if _, err := p.Render(context.Background(), input("hold", count)); err != nil {
			t.Fatal(err)
		}
		if s := p.stats(); s.renderGCs != 1 {
			t.Fatalf("after render %d, %d renders ran a GC, want only the first", i, s.renderGCs)
		}
	}
	idleVM(p, func(vm *pooledVM) {
		if got := vm.GCThreshold(); got <= 2*vm.live || got > vm.live/2+p.memory/2 {
			t.Fatalf("GC threshold %d, want more than twice the live size %d and under the limit", got, vm.live)
		}
	})
}

// Close waits for a GC between renders, then closes the VM.
func TestCloseWaitsForGCBetweenRenders(t *testing.T) {
	p, err := NewPool(testSource(t), nil, testConfig(1), false)
	if err != nil {
		t.Fatal(err)
	}
	started, finish := make(chan struct{}), make(chan struct{})
	p.collecting = func(*pooledVM) {
		close(started)
		<-finish
	}
	for i, collecting := 0, false; !collecting; i++ {
		if i == 100 {
			t.Fatal("no GC between 100 renders of garbage")
		}
		if _, err := p.Render(context.Background(), input("garbage", "")); err != nil {
			t.Fatal(err)
		}
		// A VM that needs no GC is idle when Render returns.
		select {
		case <-started:
			collecting = true
		case vm := <-p.idle:
			p.idle <- vm
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned during a GC: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(finish)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.created != 1 || s.closed != 1 {
		t.Fatalf("the VM of the GC was left open: %+v", s)
	}
}

// A render that holds far more than the live size raises the threshold at most to four times the
// live size, and the renders after it, which hold little, bring it back down toward twice.
func TestGCThresholdComesBackAfterABigRender(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	var live uintptr
	idleVM(p, func(vm *pooledVM) { live = vm.live })
	if _, err := p.Render(context.Background(), input("hold", fmt.Sprint(3*live/objectSize(t, holdJS)))); err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.renderGCs == 0 {
		t.Fatalf("a render that holds three times the live size ran no GC: %+v", s)
	}
	idleVM(p, func(vm *pooledVM) {
		if got := vm.GCThreshold(); got > 4*vm.live {
			t.Fatalf("GC threshold %d after a big render, want at most four times the live size %d", got, vm.live)
		}
	})
	for i := range 1000 {
		if _, err := p.Render(context.Background(), input("ok", "")); err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
	}
	idleVM(p, func(vm *pooledVM) {
		if got := vm.GCThreshold(); got >= 3*vm.live {
			t.Fatalf("GC threshold %d after 1000 small renders, want under three times the live size %d", got, vm.live)
		}
	})
}

// With a memory limit under twice the live size, the threshold is under 1.5 times the live size,
// and a render of garbage makes the engine collect. A GC between renders then runs only on a VM
// over the threshold, which the next render would collect at its start.
func TestGCBetweenRendersUnderALowLimit(t *testing.T) {
	var live uintptr
	idleVM(newTestPool(t, testSource(t), testConfig(1)), func(vm *pooledVM) { live = vm.live })
	cfg := testConfig(1)
	cfg.VMMemory = int64(live * 19 / 10)
	p := newTestPool(t, testSource(t), cfg)
	p.collecting = func(vm *pooledVM) {
		if size := vm.MemorySize(); size < vm.GCThreshold() {
			t.Errorf("a GC between renders at %d bytes, under the threshold %d", size, vm.GCThreshold())
		}
	}
	for i := range 100 {
		if _, err := p.Render(context.Background(), input("garbage", "")); err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		idleVM(p, func(*pooledVM) {})
	}
	if s := p.stats(); s.renderGCs == 0 || s.idleGCs == 0 {
		t.Fatalf("want GCs during and between renders under a limit of 1.9 times the live size: %+v", s)
	}
}

// A Go panic in a GC between renders has no caller to go up to. The pool logs it and replaces the
// VM.
func TestGCPanicReplacesTheVM(t *testing.T) {
	logs := make(chan string, 10)
	defer swap(&logf, func(format string, args ...any) { logs <- fmt.Sprintf(format, args...) })()
	p := newTestPool(t, testSource(t), testConfig(1))
	p.collecting = func(*pooledVM) { panic("gc failed") }
	for i := 0; p.stats().created == 1; i++ {
		if i == 100 {
			t.Fatal("no GC between 100 renders of garbage")
		}
		if _, err := p.Render(context.Background(), input("garbage", "")); err != nil {
			t.Fatal(err)
		}
		// Wait for the GC, if any, and the replacement.
		idleVM(p, func(*pooledVM) {})
	}
	if s := p.stats(); s.created != 2 || s.closed != 1 || s.idleGCs != 0 {
		t.Fatalf("want the VM replaced, got %+v", s)
	}
	if msg := <-logs; !strings.Contains(msg, "GC between renders") || !strings.Contains(msg, "gc failed") {
		t.Fatalf("logged %q", msg)
	}
	p.collecting = nil
	if _, err := p.Render(context.Background(), input("ok", "")); err != nil {
		t.Fatal(err)
	}
}

// Once Close has started, a VM that needs a GC goes back to the pool as it is, for Close to close.
func TestNoGCBetweenRendersOnceClosing(t *testing.T) {
	var live uintptr
	idleVM(newTestPool(t, testSource(t), testConfig(1)), func(vm *pooledVM) { live = vm.live })
	// About 0.75 times the live size: no GC during the render, one after it.
	count := fmt.Sprint(live * 3 / 4 / objectSize(t, cyclesJS))
	open := newTestPool(t, testSource(t), testConfig(1))
	if _, err := open.Render(context.Background(), input("cycles", count)); err != nil {
		t.Fatal(err)
	}
	idleVM(open, func(*pooledVM) {})
	if s := open.stats(); s.renderGCs != 0 || s.idleGCs != 1 {
		t.Fatalf("want the render to leave a GC between renders, got %+v", s)
	}

	p, err := NewPool(testSource(t), nil, testConfig(1), false)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	p.acquired = func() {
		go func() { closed <- p.Close() }()
		<-p.done
	}
	p.collecting = func(*pooledVM) { t.Error("a GC between renders after Close started") }
	if _, err := p.Render(context.Background(), input("cycles", count)); err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.created != 1 || s.closed != 1 || s.idleGCs != 0 {
		t.Fatalf("want the VM closed without a GC, got %+v", s)
	}
}
