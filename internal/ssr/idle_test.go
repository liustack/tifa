package ssr

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/slowjs"
)

// waitFor polls until cond holds or the time is up, then fails the test with what.
func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("after %s: %s", limit, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// renderBig renders a page that holds nearly the live size of the VM until it ends, so the VM takes
// new memory from the system and holds it free afterwards. It stays under the threshold, where the
// engine would collect during the render and the pool would give the memory back at once.
func renderBig(t *testing.T, p *Pool) {
	t.Helper()
	var live uintptr
	idleVM(p, func(vm *pooledVM) { live = vm.live })
	if _, err := p.Render(context.Background(), input("hold", fmt.Sprint(9*live/10/objectSize(t, holdJS)))); err != nil {
		t.Fatal(err)
	}
}

// A VM that waited idle for idleRelease gives back the memory it keeps free, and the engine counts
// what it gave back. The engine of lib takes its memory from libc and gives none back, so there the
// pool only counts the release.
func TestIdleVMGivesBackFreeMemory(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	renderBig(t, p)
	var before slowjs.MemoryUsage
	idleVM(p, func(vm *pooledVM) { before = vm.MemoryUsage() })
	waitFor(t, 5*idleRelease, "no idle VM was released", func() bool { return p.stats().trimmed == 1 })
	idleVM(p, func(vm *pooledVM) {
		if before.Footprint == 0 {
			return
		}
		u := vm.MemoryUsage()
		if u.ReleaseCandidate <= before.ReleaseCandidate {
			t.Fatalf("the idle VM kept its memory: offered back %d, %d before, footprint %d, %d before",
				u.ReleaseCandidate, before.ReleaseCandidate, u.Footprint, before.Footprint)
		}
	})
	// The VM is clean until it renders again: it is not released twice.
	time.Sleep(idleRelease + idleRelease/2)
	if n := p.stats().trimmed; n != 1 {
		t.Fatalf("an idle VM was released %d times", n)
	}
}

// A VM that renders again before idleRelease has passed keeps its memory: giving it back would make
// the next render take it again.
func TestBusyVMKeepsItsMemory(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	end := time.Now().Add(2*idleRelease + idleRelease/2)
	for i := 0; time.Now().Before(end); i++ {
		if _, err := p.Render(context.Background(), input("ok", fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
		time.Sleep(idleRelease / 5)
	}
	if n := p.stats().trimmed; n != 0 {
		t.Fatalf("a VM that rendered every %s was released %d times", idleRelease/5, n)
	}
}

// Each VM is released when its own time comes: one that rendered again in between waits another
// idleRelease.
func TestEachIdleVMIsReleasedWhenItsTimeComes(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(2))
	for i := range 2 {
		if _, err := p.Render(context.Background(), input("ok", fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(idleRelease / 2)
	// The VM that waited longest renders, and starts waiting again.
	if _, err := p.Render(context.Background(), input("ok", "again")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(idleRelease/2 + idleRelease/4)
	if n := p.stats().trimmed; n != 1 {
		t.Fatalf("%d VMs released after 1.25 times idleRelease, want the one that did not render again", n)
	}
	waitFor(t, 3*idleRelease, "the second VM was not released", func() bool { return p.stats().trimmed == 2 })
}

// A render that arrives while the pool gives a VM back waits for it: the pool holds the VM like a
// render does, so the two never use it together. Run with -race.
func TestRenderWaitsForAVMBeingReleased(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	taken, release := holdReleases(t, p)
	if _, err := p.Render(context.Background(), input("ok", "")); err != nil {
		t.Fatal(err)
	}
	waitTaken(t, taken)
	rendered := make(chan string, 1)
	go func() {
		html, err := p.Render(context.Background(), input("ok", "after"))
		if err != nil {
			html = err.Error()
		}
		rendered <- html
	}()
	select {
	case html := <-rendered:
		t.Fatalf("a render used the VM while it was being released: %s", html)
	case <-time.After(200 * time.Millisecond):
	}
	release()
	if html := <-rendered; !strings.Contains(html, ">after</main>") {
		t.Fatalf("the render after the release: %s", html)
	}
	if n := p.stats().trimmed; n != 1 {
		t.Fatalf("%d releases", n)
	}
}

// Close waits for a release in progress and closes the VM after it, and a pool that is closed has
// no timer and no goroutine of the releases left.
func TestCloseWaitsForAReleaseAndStopsTheTimer(t *testing.T) {
	before := runtime.NumGoroutine()
	p, err := NewPool(testSource(t), nil, testConfig(1), false)
	if err != nil {
		t.Fatal(err)
	}
	taken, release := holdReleases(t, p)
	if _, err := p.Render(context.Background(), input("ok", "")); err != nil {
		t.Fatal(err)
	}
	waitTaken(t, taken)
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned during a release: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	release()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.created != 1 || s.closed != 1 {
		t.Fatalf("the VM was not closed once: %+v", s)
	}
	p.mu.Lock()
	armed := p.trimTimer.Stop()
	p.mu.Unlock()
	if armed {
		t.Fatal("the timer of the releases was still running after Close")
	}
	waitFor(t, 5*time.Second, "goroutines left after Close", func() bool { return runtime.NumGoroutine() <= before })
}

// A pool closed before a VM has waited idleRelease never releases it.
func TestCloseBeforeTheTimerNeverReleases(t *testing.T) {
	p, err := NewPool(testSource(t), nil, testConfig(1), false)
	if err != nil {
		t.Fatal(err)
	}
	p.releasing = func(*pooledVM) { t.Error("a VM was released after Close") }
	if _, err := p.Render(context.Background(), input("ok", "")); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(idleRelease + idleRelease/2)
	if n := p.stats().trimmed; n != 0 {
		t.Fatalf("%d releases after Close", n)
	}
}

// holdReleases makes the pool wait in its first release of a VM, with the VM taken: taken is closed
// when it does, and release lets it go on (it is also called when the test ends).
func holdReleases(t *testing.T, p *Pool) (taken chan struct{}, release func()) {
	taken, gate := make(chan struct{}), make(chan struct{})
	var once, open sync.Once
	p.releasing = func(*pooledVM) {
		once.Do(func() { close(taken) })
		<-gate
	}
	release = func() { open.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return taken, release
}

// waitTaken waits for the pool to take a VM to give its memory back.
func waitTaken(t *testing.T, taken chan struct{}) {
	t.Helper()
	select {
	case <-taken:
	case <-time.After(5 * idleRelease):
		t.Fatalf("the pool did not give memory back after %s", 5*idleRelease)
	}
}

// onceClose returns a function that closes ch the first time it is called.
func onceClose(ch chan struct{}) func() {
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// statsIn returns p.stats(), or fails the test when it takes longer than the limit: the pool is stuck
// on its lock.
func statsIn(t *testing.T, p *Pool, limit time.Duration) poolStats {
	t.Helper()
	got := make(chan poolStats, 1)
	go func() { got <- p.stats() }()
	select {
	case s := <-got:
		return s
	case <-time.After(limit):
		t.Fatalf("the pool did not answer in %s: it is stuck on its lock", limit)
		return poolStats{}
	}
}

// The release of an idle VM takes the VM from the idle channel without the lock, while the replacement of
// a growing VM counts the idle VMs under the lock and then takes one. Run in this order, in a pool of one
// VM, the replacement waited for the VM with the lock held and the release for the lock, and the pool
// stopped. When the release hands the VM back, the replacement that found none has to be woken to
// exchange it, or it waits for the next render. Each step waits for the one before it, with the hooks
// of the pool, so that the order does not depend on timing.
func TestIdleReleaseAndSwapDoNotWaitForEachOther(t *testing.T) {
	p, err := NewPool(leakSource, nil, testConfig(1), false)
	if err != nil {
		t.Fatal(err)
	}
	// A pool that is stuck cannot be closed: only close the one that is not.
	t.Cleanup(func() {
		if !t.Failed() {
			if err := p.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	loadStarted, loadGo := make(chan struct{}), make(chan struct{})
	trimmingIn, trimmingGo := make(chan struct{}), make(chan struct{})
	scanned, scanGo := make(chan struct{}), make(chan struct{})
	taken := make(chan struct{})
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		onceClose(loadStarted)()
		<-loadGo
		return openVM(p)
	})()
	var scans atomic.Int32
	p.trimming = func() {
		onceClose(trimmingIn)()
		<-trimmingGo
	}
	p.scanning = func() {
		if scans.Add(1) == 2 {
			onceClose(scanned)()
			<-scanGo
		}
	}
	p.releasing = func(*pooledVM) { onceClose(taken)() }
	// The VM grows, so the replacement starts and waits for the load of the spare. The VM renders once
	// more, so that it has memory to give back, and the timer of the release runs after a second.
	leakRender(t, p)
	awaitSwap(t, loadStarted)
	if html, err := p.Render(context.Background(), Input{URL: "/ok"}); err != nil || html != "<p>ok</p>" {
		t.Fatalf("render: %s %v", html, err)
	}
	awaitSwap(t, trimmingIn)
	// The spare is loaded, and the replacement counts the idle VMs: one.
	close(loadGo)
	awaitSwap(t, scanned)
	// The release takes that VM.
	close(trimmingGo)
	awaitSwap(t, taken)
	// The replacement goes on to take a VM that is no longer there.
	close(scanGo)
	waitFor(t, 5*time.Second, "the idle VM was not exchanged for the spare", func() bool {
		return statsIn(t, p, 5*time.Second).replaced == 1
	})
	if s := statsIn(t, p, 5*time.Second); s.trimmed != 1 {
		t.Fatalf("%d releases, want 1: %+v", s.trimmed, s)
	}
	done := make(chan error, 1)
	go func() {
		_, err := p.Render(context.Background(), Input{URL: "/ok"})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a render did not get a VM")
	}
}

// The release hands the VM back as a VM that came back to the pool: it wakes the replacement, which may
// have looked for the VM while the release held it, and it keeps the time the VM went idle.
func TestIdleReleaseWakesTheSwapAndKeepsIdleSince(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	// Pretend a replacement is running, so that a VM coming back wakes it. Nothing reads the signal.
	p.mu.Lock()
	p.swapping = true
	p.mu.Unlock()
	if _, err := p.Render(context.Background(), input("ok", "")); err != nil {
		t.Fatal(err)
	}
	var since time.Time
	idleVM(p, func(vm *pooledVM) { since = vm.idleSince })
	select {
	case <-p.swapWake:
	default:
	}
	waitFor(t, 5*idleRelease, "the idle VM was not released", func() bool { return statsIn(t, p, time.Second).trimmed == 1 })
	waitFor(t, 5*time.Second, "the replacement was not woken when the release gave the VM back", func() bool { return len(p.swapWake) == 1 })
	idleVM(p, func(vm *pooledVM) {
		if !vm.idleSince.Equal(since) {
			t.Fatalf("the VM went idle at %v and the release made it %v", since, vm.idleSince)
		}
	})
}
