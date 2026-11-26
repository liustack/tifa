package ssr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const leakSource = `globalThis.held = []; globalThis.startRender = (json) => {
 const input = JSON.parse(json);
 if (input.url === '/throw') throw new Error('broken');
 const count = input.url === '/leak' ? 4000 : input.url === '/small-leak' ? 128 : 0;
 for (let i=0; i<count; i++) held.push({i, text: 'retained'});
 globalThis.renderResult = '<p>ok</p>';
}`

// sizeRule makes the pool replace a VM once a full GC finds it past twice its baseline, for the tests
// of how the pool replaces a VM, which grow it once by far less than the limit.
func sizeRule(t *testing.T) {
	// A cleanup, not a defer: it runs after the cleanup that closes a pool made later, whose
	// goroutines read the bound until then.
	t.Cleanup(swap(&growthBound, func(vm *pooledVM, memory uintptr) uintptr { return min(2*vm.baseline, vm.hardBound(memory)) }))
}

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
	sizeRule(t)
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
	sizeRule(t)
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
	sizeRule(t)
	p := newTestPool(t, leakSource, testConfig(1))
	collecting, finishGC := make(chan struct{}), make(chan struct{})
	var first sync.Once
	p.collecting = func(*pooledVM) { first.Do(func() { close(collecting); <-finishGC }) }
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
	sizeRule(t)
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
	sizeRule(t)
	p := newTestPool(t, leakSource, testConfig(1))
	defer swap(&retryDelay, 20*time.Millisecond)()
	var calls atomic.Int32
	messages := make(chan string, 8)
	attempts := make(chan time.Time, 3)
	defer captureLogs(func(msg string) { messages <- msg })()
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

// A Go panic while the spare loads is retried as a load error is, on the goroutine of the
// replacement, and the process goes on.
func TestMemorySwapPanicInLoadIsRetried(t *testing.T) {
	sizeRule(t)
	p := newTestPool(t, leakSource, testConfig(1))
	defer swap(&retryDelay, time.Millisecond)()
	messages := make(chan string, 8)
	defer captureLogs(func(msg string) {
		select {
		case messages <- msg:
		default:
		}
	})()
	var once sync.Once
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		once.Do(func() { panic("fault while loading") })
		return openVM(p)
	})()
	defer p.Close()
	leakRender(t, p)
	select {
	case msg := <-messages:
		if !strings.Contains(msg, "panic: fault while loading") {
			t.Fatal(msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no failure log")
	}
	waitReplacements(t, p, 1)
}

func TestMemorySwapManyVMsLoadOneAtATime(t *testing.T) {
	sizeRule(t)
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
	for _, tc := range []struct{ baseline, memory, want uintptr }{{100, 1000, 500}, {100, 400, 200}, {100, 300, 175}, {100, 120, 175}} {
		vm := pooledVM{baseline: tc.baseline}
		if got := vm.hardBound(tc.memory); got != tc.want {
			t.Errorf("%+v: bound %d", tc, got)
		}
	}
}

func TestMemorySwapCloseStopsBackoff(t *testing.T) {
	sizeRule(t)
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
	sizeRule(t)
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
	sizeRule(t)
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
	time.Sleep(20 * time.Millisecond)
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
	sizeRule(t)
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

// Proactive swaps must close every retired VM. Each VM loaded in place of a grown one first runs
// refitRenders renders that keep nothing, so its leak is a new one to replace and not a sign that
// the app does not fit its limit, which would make the pool wait before it replaces again.
func TestSoakMemorySwap(t *testing.T) {
	sizeRule(t)
	skipUnlessSoak(t)
	p := newTestPool(t, leakSource, testConfig(1))
	for cycle := range 16 {
		if cycle > 0 {
			for range refitRenders {
				if _, err := p.Render(context.Background(), Input{}); err != nil {
					t.Fatal(err)
				}
			}
		}
		leakRender(t, p)
		waitReplacements(t, p, cycle+1)
	}
	if e := p.Close(); e != nil {
		t.Fatal(e)
	}
	if s := p.stats(); s.created != s.closed {
		t.Fatalf("VMs left open after swaps: %+v", s)
	}
}

// putIdle must not touch a VM after it sent it: the next owner may already write it.
// Under -race the reads after the send show up as a data race.
func TestPutIdleDoesNotReadVMAfterSend(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(1))
	defer p.Close()
	vm := <-p.idle
	back := make(chan struct{})
	go func() {
		for range 2000 {
			v := <-p.idle
			v.live = v.baseline
			back <- struct{}{}
		}
	}()
	for range 2000 {
		p.putIdle(vm)
		<-back
	}
	p.putIdle(vm)
}

// warmSource keeps 16 KB for each of 150 pages the first time it renders it, as a cache of an app
// keyed by page does, and keeps 16 KB more on every render of /leak, as a leak does.
const warmSource = `globalThis.kept = new Map(); globalThis.held = []; globalThis.startRender = (json) => {
 const url = JSON.parse(json).url;
 const page = /^\/page\/(\d+)$/.exec(url);
 if (page && !kept.has(page[1])) kept.set(page[1], new Array(1024).fill(+page[1]));
 if (url === '/leak') held.push(new Array(1024).fill(held.length));
 globalThis.renderResult = '<p>ok</p>';
}`

// A VM that grows while it renders new pages and then stays keeps serving, though it ends above
// twice its baseline: a VM loaded in its place would grow the same way and be replaced again.
func TestMemorySwapKeepsAVMThatStopsGrowing(t *testing.T) {
	p := newTestPool(t, warmSource, testConfig(1))
	for range 5 {
		for page := range 150 {
			if _, err := p.Render(context.Background(), Input{URL: fmt.Sprintf("/page/%d", page)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	vm := <-p.idle
	live, bound := vm.live, 2*vm.baseline
	p.idle <- vm
	if live <= bound {
		t.Fatalf("live size %d not above the bound %d: the test does not test anything", live, bound)
	}
	time.Sleep(50 * time.Millisecond)
	if s := p.stats(); s.replaced != 0 || s.created != 1 {
		t.Fatalf("a VM that stopped growing was replaced: %+v", s)
	}
}

// A VM that keeps growing render after render is replaced before a render runs out of memory,
// once it holds more than half of the limit.
func TestMemorySwapReplacesAVMThatKeepsGrowing(t *testing.T) {
	p := newTestPool(t, warmSource, testConfig(1))
	for i := range 1000 {
		if _, err := p.Render(context.Background(), Input{URL: "/leak"}); err != nil {
			t.Fatalf("render %d: %v", i+1, err)
		}
		// The pool starts to replace it once it counts as grown, and swaps when the VM is idle.
		p.mu.Lock()
		swapping := p.swapping
		p.mu.Unlock()
		if swapping {
			t.Logf("replacing after %d renders", i+1)
			waitReplacements(t, p, 1)
			return
		}
	}
	t.Fatalf("a VM that kept growing was not replaced: %+v", p.stats())
}

// misfitSource keeps 1 MiB the first time a VM renders, as an app whose pages need more than its
// limit leaves does (with sizeRule).
const misfitSource = `globalThis.kept = null; globalThis.startRender = (json) => {
 if (kept === null) kept = new Array(64 * 1024).fill(1);
 globalThis.renderResult = '<p>ok</p>';
}`

// When the VM that replaced a grown VM grows past the bound too, a fresh VM would not fit either:
// the pool says so once and waits before it replaces a VM again, where it replaced one after
// another.
func TestMemorySwapWaitsWhenAFreshVMDoesNotFit(t *testing.T) {
	sizeRule(t)
	var logged atomic.Int32
	logger := slog.Default()
	slog.SetDefault(slog.New(countingHandler{slog.NewTextHandler(os.Stderr, nil), "raise ssr.vm_memory", &logged}))
	defer slog.SetDefault(logger)
	p := newTestPool(t, misfitSource, testConfig(1))
	render := func() {
		t.Helper()
		if _, err := p.Render(context.Background(), Input{}); err != nil {
			t.Fatal(err)
		}
	}
	render()
	waitReplacements(t, p, 1)
	for range 20 {
		render()
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if s := p.stats(); s.replaced != 1 || s.created != 2 {
		t.Errorf("replaced a VM that did not fit again: %+v", s)
	}
	if n := logged.Load(); n != 1 {
		t.Errorf("said %d times that the app needs a larger ssr.vm_memory", n)
	}
	p.mu.Lock()
	pause := time.Until(p.refitAfter)
	p.mu.Unlock()
	if pause < 50*time.Second {
		t.Errorf("waits %v before it replaces a VM again", pause)
	}
}

// countingHandler counts the records of the default logger whose message has text.
type countingHandler struct {
	slog.Handler
	text string
	n    *atomic.Int32
}

func (h countingHandler) Handle(ctx context.Context, r slog.Record) error {
	if strings.Contains(r.Message, h.text) {
		h.n.Add(1)
	}
	return h.Handler.Handle(ctx, r)
}

// liveSum is the sum of what the VMs held after their last collection, read from the VMs themselves.
// It waits until every VM is idle, and puts them back.
func liveSum(t *testing.T, p *Pool, vms int) (live, baseline int64) {
	t.Helper()
	held := make([]*pooledVM, 0, vms)
	for range vms {
		select {
		case vm := <-p.idle:
			held = append(held, vm)
			live += int64(vm.live)
			baseline += int64(vm.baseline)
		case <-time.After(5 * time.Second):
			t.Fatal("VMs not idle")
		}
	}
	for _, vm := range held {
		p.idle <- vm
	}
	return live, baseline
}

// Stats.Memory is what the loaded VMs hold by the engine's count, the sum of the size each had after
// its last collection: the baselines after loading, more after a render that keeps memory.
func TestStatsMemoryFollowsTheVMs(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(2))
	_, baseline := liveSum(t, p, 2)
	if got := p.Stats().Memory; got != baseline || baseline <= 0 {
		t.Fatalf("Memory %d, the baselines of the VMs %d", got, baseline)
	}
	leakRender(t, p)
	deadline := time.Now().Add(5 * time.Second)
	for p.Stats().Memory <= baseline {
		if time.Now().After(deadline) {
			t.Fatalf("Memory %d did not move above %d after a render that keeps memory", p.Stats().Memory, baseline)
		}
		time.Sleep(time.Millisecond)
	}
	live, _ := liveSum(t, p, 2)
	if got := p.Stats().Memory; got != live {
		t.Fatalf("Memory %d, the live sizes of the VMs %d", got, live)
	}
}

// Stats.Memory is read while renders and collections run, and a VM that closes takes its size out.
func TestStatsMemoryIsSafeAndDropsClosedVMs(t *testing.T) {
	p := newTestPool(t, leakSource, testConfig(2))
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = p.Stats()
			}
		}
	})
	for range 20 {
		if _, err := p.Render(context.Background(), Input{URL: "/small-leak"}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	liveSum(t, p, 2)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if got := p.Stats().Memory; got != 0 {
		t.Fatalf("Memory %d after Close", got)
	}
}

// Stats.Replaced counts the VMs the pool replaced because they grew.
func TestStatsReplacedCountsGrownVMs(t *testing.T) {
	sizeRule(t)
	p := newTestPool(t, leakSource, testConfig(1))
	if got := p.Stats().Replaced; got != 0 {
		t.Fatalf("Replaced %d before a swap", got)
	}
	leakRender(t, p)
	waitReplacements(t, p, 1)
	if got := p.Stats().Replaced; got != 1 {
		t.Fatalf("Replaced %d after one swap", got)
	}
}
