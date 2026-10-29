package ssr

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liustack/slowjs"
)

// The objects of the cycles and hold renders of testdata/app.js.
const (
	cyclesJS = "for (let i = 0; i < 10000; i++) { const node = { index: i }; node.self = node }; undefined"
	holdJS   = "globalThis.held = []; for (let i = 0; i < 10000; i++) held.push({ index: i }); undefined"
)

// The objects of the oldcycles render of testdata/app.js, which only a full GC frees.
const oldCyclesJS = "globalThis.oldCycle = []; oldCycle.self = oldCycle; for (let i = 0; i < 10000; i++) oldCycle.push({ index: i }); undefined"

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
	defer bytecode.Close()
	vm, err := Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	vm.SetGCThreshold(^uintptr(0))
	before := vm.MemorySize()
	if _, err := vm.Eval(js, slowjs.EvalGlobal); err != nil {
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

// Each garbage render leaves cycles that only a GC frees. The pool runs a young GC after each render,
// which frees them, so no render collects, the VM holds about what it held after Load, and the
// threshold for the next render is twice that. A full GC runs only every fullGCEvery renders.
func TestPoolRunsAYoungGCAfterEachRender(t *testing.T) {
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
	if want := 300 / fullGCEvery; s.idleGCs != want || s.youngGCs != 300-want {
		t.Fatalf("%d young and %d full GCs between 300 renders of garbage, want %d and %d", s.youngGCs, s.idleGCs, 300-want, want)
	}
	if s.renderGCs != 0 {
		t.Fatalf("%d renders ran a GC", s.renderGCs)
	}
}

// Objects a render lets go of after a young GC made them old stay until a full GC. The pool runs
// one when a young GC leaves 1.5 times the live size of the last full GC, so the VM never stays
// over that, and no render collects.
func TestFullGCWhenOldGarbageGrows(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	var live uintptr
	idleVM(p, func(vm *pooledVM) { live = vm.live })
	// Each render leaves about 0.3 times the live size that only a full GC frees.
	count := fmt.Sprint(live * 3 / 10 / objectSize(t, oldCyclesJS))
	for i := range 20 {
		if _, err := p.Render(context.Background(), input("oldcycles", count)); err != nil {
			t.Fatal(err)
		}
		idleVM(p, func(vm *pooledVM) {
			if size := vm.MemorySize(); size >= vm.live+vm.live/2 {
				t.Fatalf("render %d: an idle VM holds %d bytes, 1.5 times the live size %d or more", i, size, vm.live)
			}
		})
	}
	s := p.stats()
	if s.renderGCs != 0 || s.youngGCs+s.idleGCs != 20 || s.idleGCs < 5 || s.idleGCs > 10 {
		t.Fatalf("want a young or full GC after each of 20 renders, a full one every two or three, none during renders: %+v", s)
	}
}

// Old garbage that stays under 1.5 times the live size, such as the objects of a WeakMap whose
// key went old, still goes at a full GC every fullGCEvery renders.
func TestFullGCEveryNRenders(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	for i := range fullGCEvery {
		if _, err := p.Render(context.Background(), input("ok", "")); err != nil {
			t.Fatal(err)
		}
		idleVM(p, func(*pooledVM) {})
		s := p.stats()
		if want := 0; i == fullGCEvery-1 {
			want = 1
			if s.idleGCs != want || s.youngGCs != fullGCEvery-1 {
				t.Fatalf("after %d renders: %+v, want one full GC", i+1, s)
			}
		} else if s.idleGCs != want || s.youngGCs != i+1 {
			t.Fatalf("after %d renders: %+v, want only young GCs", i+1, s)
		}
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
	defer bytecode.Close()
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
	if s := p.stats(); s.created != 1 || s.youngGCs+s.idleGCs != 100 {
		t.Fatalf("want one VM and a GC after each render, got %+v", s)
	}
}

// A render that makes more garbage than the threshold leaves room for collects during the render.
// The engine then sets its own threshold, 1.5 times what its GC left, which counts what the render
// held. The pool runs a full GC after such a render, to know the live size again, and puts its own
// threshold back before the next render.
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
	idleVM(p, func(vm *pooledVM) {
		if got := vm.GCThreshold(); got != 2*vm.live {
			t.Fatalf("GC threshold %d after the engine's GC, want twice the live size %d", got, vm.live)
		}
	})
	if s := p.stats(); s.renderGCs != 1 || s.idleGCs != 1 || s.youngGCs != 0 {
		t.Fatalf("want one render that ran a GC and a full GC after it, got %+v", s)
	}
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
// and a render of garbage makes the engine collect, so a full GC follows each. Old garbage, which
// young GCs keep, is collected during renders or between them, before it fills the VM.
func TestGCBetweenRendersUnderALowLimit(t *testing.T) {
	var live uintptr
	idleVM(newTestPool(t, testSource(t), testConfig(1)), func(vm *pooledVM) { live = vm.live })
	cfg := testConfig(1)
	cfg.VMMemory = int64(live * 19 / 10)
	p := newTestPool(t, testSource(t), cfg)
	for i := range 100 {
		if _, err := p.Render(context.Background(), input("garbage", "")); err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		idleVM(p, func(*pooledVM) {})
	}
	if s := p.stats(); s.renderGCs != 100 || s.idleGCs != 100 || s.youngGCs != 0 {
		t.Fatalf("want a GC during and a full GC after each render of garbage: %+v", s)
	}
	count := fmt.Sprint(live / 10 / objectSize(t, oldCyclesJS))
	for i := range 100 {
		if _, err := p.Render(context.Background(), input("oldcycles", count)); err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		idleVM(p, func(*pooledVM) {})
	}
	if s := p.stats(); s.created != 1 || s.renderGCs+s.idleGCs <= 200+100/fullGCEvery {
		t.Fatalf("want old garbage collected in one VM: %+v", s)
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
	if s := p.stats(); s.created != 2 || s.closed != 1 || s.youngGCs != 0 || s.idleGCs != 0 {
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
	// About 0.75 times the live size: no GC during the render, a young one after it.
	count := fmt.Sprint(live * 3 / 4 / objectSize(t, cyclesJS))
	open := newTestPool(t, testSource(t), testConfig(1))
	if _, err := open.Render(context.Background(), input("cycles", count)); err != nil {
		t.Fatal(err)
	}
	idleVM(open, func(vm *pooledVM) {
		if size := vm.MemorySize(); size >= vm.live+vm.live/8 {
			t.Fatalf("a young GC left %d bytes of a live size of %d", size, vm.live)
		}
	})
	if s := open.stats(); s.renderGCs != 0 || s.youngGCs != 1 || s.idleGCs != 0 {
		t.Fatalf("want the render to leave a young GC between renders, got %+v", s)
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
	if s := p.stats(); s.created != 1 || s.closed != 1 || s.youngGCs != 0 || s.idleGCs != 0 {
		t.Fatalf("want the VM closed without a GC, got %+v", s)
	}
}

// Load gives the memory that loading left free back to the system, so that a VM the pool keeps
// for later holds what its bundle keeps. The engine of lib takes its memory from libc and gives
// none back.
func TestLoadGivesBackFreeMemory(t *testing.T) {
	cfg := testConfig(1)
	bytecode, err := Compile(testSource(t), uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer bytecode.Close()
	vm, err := Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	u := vm.MemoryUsage()
	if u.Footprint == 0 {
		t.Skip("the engine takes its memory from libc")
	}
	if u.ReleaseCandidate == 0 {
		t.Fatalf("Load gave nothing back: footprint %d, %d bytes in use", u.Footprint, u.MallocSize)
	}
}

// A render that holds far more than the live size raises the threshold, and the VM grows to fit
// it. The full GC after the render gives all that memory back though the threshold stays raised,
// since the VM takes what it needs again when the next big render comes.
func TestFullGCGivesBackWhatABigRenderTook(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	var live uintptr
	var before slowjs.MemoryUsage
	idleVM(p, func(vm *pooledVM) { live, before = vm.live, vm.MemoryUsage() })
	if before.Footprint == 0 {
		t.Skip("the engine takes its memory from libc")
	}
	if _, err := p.Render(context.Background(), input("hold", fmt.Sprint(3*live/objectSize(t, holdJS)))); err != nil {
		t.Fatal(err)
	}
	idleVM(p, func(vm *pooledVM) {
		u := vm.MemoryUsage()
		if s := p.stats(); s.idleGCs != 1 {
			t.Fatalf("want one full GC after the big render, got %+v", s)
		}
		if vm.GCThreshold() <= 2*vm.live {
			t.Fatalf("the threshold %d is back to twice the live size %d, the render did not raise it", vm.GCThreshold(), vm.live)
		}
		if given := u.ReleaseCandidate - before.ReleaseCandidate; given < int64(live/2) {
			t.Fatalf("the full GC gave back %d bytes, live size %d, threshold %d, footprint %d before the render, %d now",
				given, live, vm.GCThreshold(), before.Footprint, u.Footprint)
		}
		if u.Footprint > before.Footprint {
			t.Fatalf("the VM still holds %d bytes of chunks after the big render, %d before it", u.Footprint, before.Footprint)
		}
	})
}

// A full GC between renders gives back all the free memory of the VM, also when the threshold
// leaves room for more than the VM holds free. The renders are big enough to leave free memory
// of a bit less than the live size.
func TestFullGCGivesBackFreeMemory(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	var live uintptr
	idleVM(p, func(vm *pooledVM) { live = vm.live })
	count := fmt.Sprint(live / 2 / objectSize(t, holdJS))
	for i := range fullGCEvery - 1 {
		if _, err := p.Render(context.Background(), input("hold", count)); err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
	}
	var before slowjs.MemoryUsage
	idleVM(p, func(vm *pooledVM) { before = vm.MemoryUsage() })
	if before.Footprint == 0 {
		t.Skip("the engine takes its memory from libc")
	}
	if _, err := p.Render(context.Background(), input("hold", count)); err != nil {
		t.Fatal(err)
	}
	idleVM(p, func(vm *pooledVM) {
		u := vm.MemoryUsage()
		if s := p.stats(); s.idleGCs != 1 {
			t.Fatalf("want one full GC, got %+v", s)
		}
		if given := u.ReleaseCandidate - before.ReleaseCandidate; given < int64(vm.live/4) {
			t.Fatalf("the full GC gave back %d bytes, live size %d, threshold %d, footprint %d", given, vm.live, vm.GCThreshold(), u.Footprint)
		}
	})
}
