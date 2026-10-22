package ssr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/libc"
)

const leakSource = `globalThis.held = []; globalThis.startRender = (json) => {
 const input = JSON.parse(json);
 if (input.url === '/throw') throw new Error('broken');
 const count = input.url === '/leak' ? 4000 : input.url === '/small-leak' ? 128 : 0;
 for (let i=0; i<count; i++) held.push({i, text: 'retained'});
 globalThis.renderResult = '<p>ok</p>';
}`

func awaitSwap(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement did not progress")
	}
}

func leakRender(t *testing.T, p *Pool) {
	t.Helper()
	if _, err := p.Render(context.Background(), Input{URL: "/leak"}); err != nil {
		t.Fatal(err)
	}
}

func TestMemorySwapLoadsBeforeRetiring(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	started, finish := make(chan struct{}), make(chan struct{})
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		close(started)
		vm, err := openVM(p)
		<-finish
		return vm, err
	})()
	defer p.Close()
	defer close(finish)
	leakRender(t, p)
	awaitSwap(t, started)
	for range 20 {
		if html, err := p.Render(context.Background(), Input{URL: "/ok"}); err != nil || html != "<p>ok</p>" {
			t.Fatalf("during load: %s %v", html, err)
		}
	}
	if s := p.stats(); s.created > 2 || s.closed != 0 {
		t.Fatalf("old VM lost during load: %+v", s)
	}
}

func TestMemorySwapWaitsForRenderAndQueue(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprint(queued), func(t *testing.T) {
			p := newTestPool(t, leakSource, testConfig(1))
			started, finish, loaded := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
				close(started)
				<-finish
				v, e := openVM(p)
				close(loaded)
				return v, e
			})()
			defer p.Close()
			var block atomic.Bool
			entered, release := make(chan struct{}, 2), make(chan struct{})
			p.acquired = func() {
				if block.Load() {
					entered <- struct{}{}
					<-release
				}
			}
			waiting := make(chan struct{}, 1)
			p.waiting = func(*time.Timer) { waiting <- struct{}{} }
			leakRender(t, p)
			awaitSwap(t, started)
			block.Store(true)
			result := make(chan error, 2)
			go func() { _, e := p.Render(context.Background(), Input{}); result <- e }()
			awaitSwap(t, entered)
			if queued {
				go func() { _, e := p.Render(context.Background(), Input{}); result <- e }()
				awaitSwap(t, waiting)
			}
			close(finish)
			awaitSwap(t, loaded)
			if s := p.stats(); s.closed != 0 {
				t.Fatalf("closed a rendering VM: %+v", s)
			}
			block.Store(false)
			close(release)
			if e := <-result; e != nil {
				t.Fatal(e)
			}
			if queued {
				if e := <-result; e != nil {
					t.Fatal(e)
				}
			}
			waitReplacements(t, p, 1)
			if s := p.stats(); s.peak != 2 || s.created != 2 {
				t.Fatalf("capacity changed: %+v", s)
			}
		})
	}
}

func waitReplacements(t *testing.T, p *Pool, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for p.stats().closed < n {
		if time.Now().After(deadline) {
			t.Fatalf("replacement unfinished: %+v", p.stats())
		}
		time.Sleep(time.Millisecond)
	}
	if p.stats().replaced != n {
		t.Fatalf("replaced count: %+v", p.stats())
	}
}

func TestMemorySwapDoesNotLoadWhileRequestsWait(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	collecting, finishGC := make(chan struct{}), make(chan struct{})
	p.collecting = func(*pooledVM) { close(collecting); <-finishGC }
	entered, release := make(chan struct{}), make(chan struct{})
	var hold atomic.Bool
	p.acquired = func() {
		if hold.Load() {
			close(entered)
			<-release
		}
	}
	waiting, admit := make(chan struct{}), make(chan struct{})
	p.waiting = func(*time.Timer) { close(waiting); <-admit }
	loaded := make(chan struct{}, 1)
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) { loaded <- struct{}{}; return openVM(p) })()
	defer p.Close()
	leakRender(t, p)
	awaitSwap(t, collecting)
	hold.Store(true)
	result := make(chan error, 1)
	go func() { _, e := p.Render(context.Background(), Input{}); result <- e }()
	awaitSwap(t, waiting)
	close(finishGC)
	// Keep the queued request before its receive, so the old VM is idle but
	// loading must still yield to that request.
	deadline := time.Now().Add(time.Second)
	for len(p.idle) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("GC did not return VM")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-loaded:
		t.Fatal("loaded with a queued request and an idle VM")
	case <-time.After(20 * time.Millisecond):
	}
	close(admit)
	awaitSwap(t, entered)
	select {
	case <-loaded:
		t.Fatal("loaded while the only VM was busy serving the queue")
	case <-time.After(20 * time.Millisecond):
	}
	hold.Store(false)
	close(release)
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	awaitSwap(t, loaded)
	waitReplacements(t, p, 1)
}

func TestMemorySwapCloseWaitsForLoad(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	started, finish := make(chan struct{}), make(chan struct{})
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) { close(started); <-finish; return openVM(p) })()
	leakRender(t, p)
	awaitSwap(t, started)
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	awaitSwap(t, p.done)
	select {
	case e := <-closed:
		t.Fatalf("Close returned during load: %v", e)
	default:
	}
	close(finish)
	if e := <-closed; e != nil {
		t.Fatal(e)
	}
	if s := p.stats(); s.created != 2 || s.closed != 2 || s.replaced != 0 {
		t.Fatalf("VM leaked at shutdown: %+v", s)
	}
}

func TestMemorySwapFailedLoadBacksOffAndLogsOnce(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	defer swap(&retryDelay, 20*time.Millisecond)()
	var calls atomic.Int32
	messages := make(chan string, 8)
	attempts := make(chan time.Time, 3)
	defer swap(&logf, func(f string, a ...any) { messages <- fmt.Sprintf(f, a...) })()
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		attempts <- time.Now()
		if calls.Add(1) <= 2 {
			return nil, errors.New("load failed")
		}
		return openVM(p)
	})()
	defer p.Close()
	leakRender(t, p)
	select {
	case msg := <-messages:
		if !strings.Contains(msg, "load failed") {
			t.Fatal(msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no failure log")
	}
	for range 10 {
		if _, e := p.Render(context.Background(), Input{}); e != nil {
			t.Fatal(e)
		}
	}
	waitReplacements(t, p, 1)
	if calls.Load() != 3 {
		t.Fatalf("load calls %d", calls.Load())
	}
	first, second, third := <-attempts, <-attempts, <-attempts
	if second.Sub(first) < 20*time.Millisecond || third.Sub(second) < 40*time.Millisecond {
		t.Fatalf("retry gaps %s, %s", second.Sub(first), third.Sub(second))
	}
	for len(messages) > 0 {
		if msg := <-messages; strings.Contains(msg, "load failed") {
			t.Fatal("logged the same failure twice")
		}
	}
}

func TestMemorySwapManyVMsLoadOneAtATime(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(3))
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	p.acquired = func() { entered <- struct{}{}; <-release }
	started := make(chan struct{}, 3)
	finish := make(chan struct{}, 3)
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) { started <- struct{}{}; <-finish; return openVM(p) })()
	defer p.Close()
	defer close(finish)
	result := make(chan error, 3)
	for range 3 {
		go func() { _, e := p.Render(context.Background(), Input{URL: "/leak"}); result <- e }()
	}
	for range 3 {
		awaitSwap(t, entered)
	}
	close(release)
	for range 3 {
		if e := <-result; e != nil {
			t.Fatal(e)
		}
	}
	for i := range 3 {
		awaitSwap(t, started)
		select {
		case <-started:
			t.Fatal("concurrent replacement loads")
		case <-time.After(10 * time.Millisecond):
		}
		finish <- struct{}{}
		waitReplacements(t, p, i+1)
	}
	if s := p.stats(); s.created != 6 || s.peak != 4 {
		t.Fatalf("more than one spare: %+v", s)
	}
}

func TestMemorySwapBound(t *testing.T) {
	for _, tc := range []struct{ baseline, memory, want uintptr }{{100, 1000, 200}, {100, 240, 180}, {100, 200, 175}, {100, 120, 175}} {
		vm := pooledVM{baseline: tc.baseline}
		if got := vm.memoryBound(tc.memory); got != tc.want {
			t.Errorf("%+v: bound %d", tc, got)
		}
	}
}

func TestMemorySwapCloseStopsBackoff(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	defer swap(&retryDelay, time.Hour)()
	failed := make(chan struct{})
	defer swap(&loadVM, func(*Pool) (*pooledVM, error) { close(failed); return nil, errors.New("broken") })()
	leakRender(t, p)
	awaitSwap(t, failed)
	closed := make(chan struct{})
	go func() { p.Close(); close(closed) }()
	awaitSwap(t, closed)
	if s := p.stats(); s.created != s.closed {
		t.Fatalf("VM leaked: %+v", s)
	}
}

func TestMemorySwapDiscardsSpareAfterRenderFailure(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-finish
		}
		return openVM(p)
	})()
	defer p.Close()
	leakRender(t, p)
	awaitSwap(t, started)
	if _, e := p.Render(context.Background(), Input{URL: "/throw"}); e == nil {
		t.Fatal("throw succeeded")
	}
	if _, e := p.Render(context.Background(), Input{}); e != nil {
		t.Fatal(e)
	}
	close(finish)
	deadline := time.Now().Add(time.Second)
	for p.stats().closed < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("unused spare retained: %+v", p.stats())
		}
		time.Sleep(time.Millisecond)
	}
	if s := p.stats(); s.replaced != 0 || s.created != 3 {
		t.Fatalf("unnecessary swap: %+v", s)
	}
}

// A queued request can be registered before its select receives an already
// returned VM. Even a ready spare must yield to it in that window.
func TestMemorySwapDefersExchangeForQueuedRequest(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	started, finish, loaded := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		close(started)
		<-finish
		v, e := openVM(p)
		close(loaded)
		return v, e
	})()
	defer p.Close()
	waiting, admit := make(chan struct{}), make(chan struct{})
	p.waiting = func(*time.Timer) { close(waiting); <-admit }
	leakRender(t, p)
	awaitSwap(t, started)
	vm := <-p.idle
	result := make(chan error, 1)
	go func() { _, e := p.Render(context.Background(), Input{}); result <- e }()
	awaitSwap(t, waiting)
	p.putIdle(vm)
	close(finish)
	awaitSwap(t, loaded)
	select {
	case <-time.After(20 * time.Millisecond):
	}
	if s := p.stats(); s.closed != 0 || s.replaced != 0 {
		t.Fatalf("exchanged with a queued request: %+v", s)
	}
	close(admit)
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	waitReplacements(t, p, 1)
}

// Small successful renders can accumulate a leak over several collections.
func TestMemorySwapAccumulatedLeak(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	started := make(chan struct{}, 1)
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) { started <- struct{}{}; return openVM(p) })()
	defer p.Close()
	for i := range 100 {
		if _, e := p.Render(context.Background(), Input{URL: "/small-leak"}); e != nil {
			t.Fatal(e)
		}
		select {
		case <-started:
			if i == 0 {
				t.Fatal("replaced before the leak accumulated")
			}
			waitReplacements(t, p, 1)
			return
		default:
		}
	}
	awaitSwap(t, started)
	waitReplacements(t, p, 1)
}

// Proactive swaps must release the native allocations of every retired VM.
func TestSoakMemorySwap(t *testing.T) {
	skipUnlessSoak(t)
	baseline := nativeBaseline(t)
	p := newTestPool(t, leakSource, testConfig(1))
	for cycle := range 16 {
		leakRender(t, p)
		waitReplacements(t, p, cycle+1)
	}
	if e := p.Close(); e != nil {
		t.Fatal(e)
	}
	if got := libc.MemStat().Allocs; got != baseline {
		t.Fatalf("native allocations after swaps: %d, baseline %d", got, baseline)
	}
}
