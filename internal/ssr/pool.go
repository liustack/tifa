// Package ssr renders pages to HTML in a pool of prewarmed QuickJS VMs.
//
// A bundle defines globalThis.startRender(json, ...texts). It receives the JSON of Input and, as
// the arguments after it, the strings of Input.Texts. It leaves globalThis.renderResult as a
// string: the HTML, which starts with <, on success, or the JSON of {"redirect": {location,
// status}} or {"error": ...}. Work may continue in microtasks, the host drains them before reading
// the result. Nothing else crosses the boundary.
package ssr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/liustack/slowjs"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
)

// Input is the page data passed to JavaScript. Pool.Render takes the clock
// separately from its context. Cacheable HTML must depend only on Input.
type Input struct {
	BuildID string          `json:"buildId"`
	URL     string          `json:"url"`
	Data    json.RawMessage `json:"data"` // the results of the Loads, already JSON, without the data of the successful ones
	// Texts are the JSON texts of the data of the successful Loads, which Data refers to by
	// their number. Each goes to startRender as an argument of its own, as it is, so that neither
	// Go escapes it into the JSON of the input nor the VM parses it out of that JSON. The VM
	// reads each text once, with JSON.parse, and the page carries the same text to the browser.
	Texts []string `json:"-"`
	// Preload is the module preloads of the matched Layouts and route, which the document
	// carries after the tags of the browser entry.
	Preload string `json:"preload,omitempty"`
}

// ErrClosed is returned by Render after Close has started.
var ErrClosed = errors.New("ssr: pool closed")

// Pool holds cfg.VMs prewarmed VMs. A VM runs one render at a time and stays
// in the pool for the next one. A VM whose render fails for any reason (a JS
// exception, the time budget, the memory limit, a Go panic) leaves the pool, and a
// background goroutine replaces it with a fresh VM loaded from the same bytecode.
// The garbage renders leave is collected between renders, after the answer, by a
// young GC after each render and a full GC now and then (see release). VMs whose
// live size grows too far are replaced after loading a spare, without taking
// capacity away from renders.
type Pool struct {
	bytecode    *slowjs.SharedBytecode
	sources     *sources
	memory      uintptr
	stack       uintptr // stack slots of each VM, see Load
	timeout     time.Duration
	waitTimeout time.Duration
	waiters     chan struct{}
	dev         bool // the checks of tifa dev, see warnIntl
	idle        chan *pooledVM
	done        chan struct{} // closed when Close starts

	mu       sync.Mutex
	closing  bool
	failure  error // errors from closing VMs, returned by Close
	counts   poolStats
	warned   map[string]bool // the Intl warnings logged
	swapping bool            // one load-first replacement, including retries and closing the old VM
	swapWake chan struct{}

	renders   sync.WaitGroup
	refills   sync.WaitGroup
	closeOnce sync.Once

	waiting    func(*time.Timer) // test hook, runs before the wait select
	acquired   func()            // test hook, runs after a render takes a VM
	collecting func(*pooledVM)   // test hook, runs before a GC between renders
}

// pooledVM is a VM of the pool with what its GC threshold is made of (see gcThreshold).
type pooledVM struct {
	*VM
	// live is the live size, what the VM held after its last full GC between renders, or after Load.
	live uintptr
	// young counts the renders since that GC.
	young int
	// baseline is the live size after Load, never raised by renders.
	baseline uintptr
	// need is the threshold the engine set when it last collected during a render, at most four
	// times the live size, or 0 before. A render that collects keeps it at least where it was, and
	// each render that does not takes 1/needDecay of what it is above twice the live size off it.
	need uintptr
	// threshold is the GC threshold the pool set last. The engine sets another when it collects.
	threshold uintptr
}

// needDecay sets how slowly need comes down after a big render. At 1/1024 a render, half of what
// need is above twice the live size goes in about 700 renders: a VM that ran one big page gets its
// usual threshold back, and a page that needs need collects during a render about once in 1000.
const needDecay = 1024

// setThreshold sets the GC threshold of vm from its live size and need under the limit memory.
func (vm *pooledVM) setThreshold(memory uintptr) {
	vm.threshold = gcThreshold(vm.live, vm.need, memory)
	vm.SetGCThreshold(vm.threshold)
}

// renderGCs counts renders with an engine GC, youngGCs collections between renders that ran only
// a young GC, idleGCs those that ran a full GC, and replaced successful proactive swaps (failure
// refills only affect created and closed).
type poolStats struct{ created, closed, peak, renderGCs, youngGCs, idleGCs, replaced int }

// fullGCEvery is how many renders a VM runs at most between two full GCs. A young GC keeps what
// the old objects hold, such as the cycles of objects that went old before a render let go of
// them, and with no full GC that waits for 1.5 times the live size. A full GC every 256 renders
// is under 0.4% of renders. On the example shop, every 64 renders raised the P99 of renders by
// 1.5% to 4%, and every 1024 gave the same P99 and memory as 256.
const fullGCEvery = 256

// Replaced by tests.
var (
	logf       = log.Printf
	retryDelay = time.Second
	loadVM     = openVM
)

const maxRetryDelay = 30 * time.Second

// NewPool compiles the bundle source once and prewarms cfg.VMs VMs from the bytecode.
// It fails if the bundle does not compile as an ES module, which is strict mode code, or its top
// level code fails in any VM. sourceMap is the source map of the bundle (ssr.js.map), nil without
// one: the errors of the bundle's code, a compile error included, give the places of their stack
// traces in the sources with it, in the bundle without it.
// dev, set under tifa dev, logs each place where the bundle calls Intl or a toLocale* method
// without a locale or, for a date, a timeZone, see warnIntl.
func NewPool(source string, sourceMap []byte, cfg config.SSR, dev bool) (*Pool, error) {
	switch {
	case cfg.VMs < 1:
		return nil, fmt.Errorf("ssr: ssr.vms must be at least 1, got %d", cfg.VMs)
	case cfg.VMMemory <= 0:
		return nil, fmt.Errorf("ssr: ssr.vm_memory must be positive, got %d", cfg.VMMemory)
	case cfg.VMStackDepth < 1 || cfg.VMStackDepth > config.MaxVMStackDepth:
		return nil, fmt.Errorf("ssr: ssr.vm_stack_depth must be from 1 to %d, got %d", config.MaxVMStackDepth, cfg.VMStackDepth)
	case cfg.Timeout <= 0:
		return nil, fmt.Errorf("ssr: ssr.timeout must be positive, got %s", cfg.Timeout)
	case cfg.QueueLimit < 0:
		return nil, fmt.Errorf("ssr: ssr.queue_limit must not be negative, got %d", cfg.QueueLimit)
	case cfg.WaitTimeout <= 0:
		return nil, fmt.Errorf("ssr: ssr.wait_timeout must be positive, got %s", cfg.WaitTimeout)
	}
	p := &Pool{
		sources:     &sources{code: source, raw: sourceMap},
		memory:      uintptr(cfg.VMMemory),
		stack:       uintptr(cfg.VMStackDepth),
		timeout:     cfg.Timeout,
		waitTimeout: cfg.WaitTimeout,
		waiters:     make(chan struct{}, cfg.QueueLimit),
		dev:         dev,
		idle:        make(chan *pooledVM, cfg.VMs),
		done:        make(chan struct{}),
		swapWake:    make(chan struct{}, 1),
		warned:      map[string]bool{},
	}
	var err error
	if p.bytecode, err = Compile(source, p.memory, p.timeout); err != nil {
		return nil, fmt.Errorf("ssr: compile bundle: %w", p.sources.explain(err, true))
	}
	for range cfg.VMs {
		vm, err := loadVM(p)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("ssr: prewarm VM: %w", p.sources.explain(err, true)), p.Close())
		}
		p.putIdle(vm)
	}
	return p, nil
}

// Render waits for an idle VM and renders in into a whole HTML document.
// Waiting honours ctx, ssr.queue_limit and ssr.wait_timeout. A full queue or an
// expired wait returns core.ErrSSRBusy. Once a VM is taken the render runs to completion
// or to the ssr.timeout budget, because stopping it halfway would cost the VM.
// A budget overrun returns an error that matches context.DeadlineExceeded. When the router
// redirects the URL, Render returns a *RedirectError.
func (p *Pool) Render(ctx context.Context, in Input) (string, error) {
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		return "", ErrClosed
	}
	p.renders.Add(1)
	p.mu.Unlock()
	defer p.renders.Done()

	vm, err := p.acquire(ctx)
	if err != nil {
		return "", err
	}
	// select picks at random among ready cases, so a canceled request or a closing
	// pool can still win the idle VM. Hand it back unused.
	if err := p.stopped(ctx); err != nil {
		p.putIdle(vm)
		return "", err
	}
	// Do not keep another copy of the input while waiting or encode rejected work.
	input, err := json.Marshal(in)
	if err != nil {
		p.putIdle(vm)
		return "", fmt.Errorf("ssr: encode input: %w", err)
	}
	// The VM goes back to the pool only when the render left it in a known state. Otherwise it
	// is replaced, also when a Go panic, from the engine or a Go function the bundle calls,
	// leaves Render: the panic goes on up and the pool keeps its size.
	keep := false
	defer func() {
		if keep {
			p.release(vm)
		} else {
			p.retire(vm)
		}
	}()
	if p.acquired != nil {
		p.acquired()
	}
	vm.SetClock(core.RenderClock(ctx))
	// Clear before release or retirement, including when a callback panics.
	defer vm.SetClock(nil)
	html, err := RenderVM(vm.VM, input, in.Texts, p.timeout)
	var redirect *RedirectError
	if err != nil && !errors.As(err, &redirect) {
		return "", p.sources.explain(err, false)
	}
	if p.dev {
		// A warning must not fail the page. A VM that cannot list them is replaced.
		if warnErr := p.warnIntl(vm.VM.VM); warnErr != nil {
			logf("tifa: ssr: read the Intl warnings: %v", warnErr)
			return html, err
		}
	}
	// A redirect is an answer, not a failure: the VM goes back to the pool.
	keep = true
	return html, err
}

func (p *Pool) acquire(ctx context.Context) (*pooledVM, error) {
	if err := p.stopped(ctx); err != nil {
		return nil, err
	}
	p.mu.Lock()
	select {
	case vm := <-p.idle:
		p.mu.Unlock()
		return vm, nil
	default:
	}
	select {
	case p.waiters <- struct{}{}:
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			<-p.waiters
			p.wakeSwap()
			p.mu.Unlock()
		}()
	default:
		p.mu.Unlock()
		return nil, fmt.Errorf("%w: queue is full", core.ErrSSRBusy)
	}
	deadline := time.Now().Add(p.waitTimeout)
	timer := time.NewTimer(p.waitTimeout)
	defer timer.Stop()
	if p.waiting != nil {
		p.waiting(timer)
	}
	select {
	case vm := <-p.idle:
		// A ready VM must not win a select after the wait budget expired.
		if !time.Now().Before(deadline) {
			p.putIdle(vm)
			return nil, fmt.Errorf("%w: wait exceeded %s", core.ErrSSRBusy, p.waitTimeout)
		}
		return vm, nil
	case <-timer.C:
		return nil, fmt.Errorf("%w: wait exceeded %s", core.ErrSSRBusy, p.waitTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.done:
		return nil, ErrClosed
	}
}

func (p *Pool) stopped(ctx context.Context) error {
	select {
	case <-p.done:
		return ErrClosed
	default:
		return ctx.Err()
	}
}

// retire closes vm and starts its replacement in the background.
// Close waits for the replacement, so it runs while a render still holds the pool open.
func (p *Pool) retire(vm *pooledVM) {
	p.refills.Go(func() {
		p.closeVM(vm)
		p.refill()
	})
}

// refill loads one VM into the pool. A failure is logged and retried with a growing
// delay until it succeeds or the pool closes, so lost capacity is never silent.
func (p *Pool) refill() {
	for delay := retryDelay; ; delay = min(2*delay, maxRetryDelay) {
		select {
		case <-p.done:
			return
		default:
		}
		vm, err := loadVM(p)
		if err == nil {
			// Failure recovery closes the old VM first. Unlike a proactive swap,
			// it must restore a slot whose VM can no longer serve requests.
			p.putIdle(vm)
			return
		}
		logf("tifa: ssr: replace VM: %v, retrying in %s", p.sources.explain(err, true), delay)
		timer := time.NewTimer(delay)
		select {
		case <-p.done:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Close stops new renders, wakes renders still waiting for a VM with ErrClosed,
// waits for renders in progress and pending replacements, then closes every VM.
// It returns the errors from closing VMs. Calling it again returns the same result.
func (p *Pool) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closing = true
		p.mu.Unlock()
		close(p.done)
		p.renders.Wait()
		p.refills.Wait()
		for {
			select {
			case vm := <-p.idle:
				p.closeVM(vm)
			default:
				// The bytecode stays for a VM loaded from it outside the pool until it is closed.
				if p.bytecode != nil {
					p.bytecode.Close()
				}
				return
			}
		}
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failure
}

func openVM(p *Pool) (*pooledVM, error) {
	vm, err := Load(p.bytecode, p.memory, p.stack, p.timeout, p.dev)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.counts.created++
	p.counts.peak = max(p.counts.peak, p.counts.created-p.counts.closed)
	p.mu.Unlock()
	// Load ended with a GC and set the threshold.
	return &pooledVM{VM: vm, live: vm.MemorySize(), baseline: vm.MemorySize(), threshold: vm.GCThreshold()}, nil
}

// release puts vm back in the pool after a render it survived, through a goroutine that collects
// it first, after the render has given its answer (see collect). A GC takes about as long as the
// garbage it frees, so collecting here takes it out of the renders, which with the threshold of
// gcThreshold rarely collect. A young GC after each render visits only the objects made since the
// last GC, mostly the garbage of that render, where a full GC visits the objects of the bundle
// too, so it takes a fraction of the time of the full GC it replaces.
func (p *Pool) release(vm *pooledVM) {
	engineGC := vm.GCThreshold() != vm.threshold
	if engineGC {
		// The engine collected during the render and set the threshold to 1.5 times what its GC
		// left, which counts what the render held then, so the live size stays the one of the last
		// GC between renders.
		vm.need = min(max(vm.need, vm.GCThreshold()), 4*vm.live)
		p.mu.Lock()
		p.counts.renderGCs++
		p.mu.Unlock()
	} else if vm.need > 2*vm.live {
		vm.need -= (vm.need - 2*vm.live) / needDecay
	}
	vm.setThreshold(p.memory)
	vm.young++
	p.refills.Go(func() { p.collect(vm, engineGC) })
}

// collect runs a GC between renders on vm and puts it back in the pool, a full one after a render
// in which the engine collected (see runGC). Close waits for it, and once Close has started vm
// goes back as it is, for Close to close it. A GC that fails replaces vm.
func (p *Pool) collect(vm *pooledVM, engineGC bool) {
	select {
	case <-p.done:
		p.putIdle(vm)
		return
	default:
	}
	if err := p.runGC(vm, engineGC); err != nil {
		logf("tifa: ssr: GC between renders: %v, replacing the VM", err)
		p.closeVM(vm)
		p.refill()
		return
	}
	p.putIdle(vm)
}

// memoryBound leaves room for transient render allocations under a low VM limit.
// The real-page measurements in docs/performance.md reach 1.67 times the baseline.
// Keep at least 75% growth room even under a low limit, so ordinary warm caches
// do not cause repeated reloads without recovering any space.
func (vm *pooledVM) memoryBound(memory uintptr) uintptr {
	return max(vm.baseline+vm.baseline/2+vm.baseline/4, min(2*vm.baseline, memory-memory/4))
}

// putIdle publishes only VMs exclusively owned by its caller. Admission and swap
// selection share mu, so a new waiter cannot race the decision to start loading.
func (p *Pool) putIdle(vm *pooledVM) {
	p.mu.Lock()
	p.idle <- vm
	if !p.closing && !p.swapping && vm.live > vm.memoryBound(p.memory) {
		p.swapping = true
		p.refills.Go(p.replaceGrown)
	}
	p.wakeSwap()
	p.mu.Unlock()
}

// Called under mu. Notifications are hints, the worker always checks the state.
func (p *Pool) wakeSwap() {
	if p.swapping {
		select {
		case p.swapWake <- struct{}{}:
		default:
		}
	}
}

// grownIdle inspects only idle VMs, while admission is locked and no requests
// wait. With fresh it exchanges one VM before releasing the lock. Without fresh
// it only checks whether loading a replacement would be useful.
func (p *Pool) grownIdle(fresh *pooledVM) *pooledVM {
	if p.closing || len(p.waiters) != 0 {
		return nil
	}
	for n := len(p.idle); n > 0; n-- {
		vm := <-p.idle
		if vm.live > vm.memoryBound(p.memory) {
			if fresh != nil {
				p.idle <- fresh
			} else {
				p.idle <- vm
			}
			return vm
		}
		p.idle <- vm
	}
	return nil
}

// replaceGrown owns the spare until it is exchanged or closed. The old VM keeps
// serving during loading and backoff. Close cancels waits and joins this worker.
func (p *Pool) replaceGrown() {
	var fresh *pooledVM
	defer func() {
		if fresh != nil {
			p.closeVM(fresh)
		}
	}()
	delay, warned := retryDelay, false
	for {
		p.mu.Lock()
		if p.closing {
			p.swapping = false
			p.mu.Unlock()
			return
		}
		var freshLive uintptr
		if fresh != nil {
			freshLive = fresh.live
		}
		old := p.grownIdle(fresh)
		if old == nil {
			// No more candidates when every VM is idle. Busy VMs will notify on return.
			if len(p.idle) == cap(p.idle) && len(p.waiters) == 0 {
				if fresh != nil {
					p.mu.Unlock()
					p.closeVM(fresh)
					fresh = nil
					continue
				}
				p.swapping = false
				p.mu.Unlock()
				return
			}
			p.mu.Unlock()
			select {
			case <-p.done:
			case <-p.swapWake:
			}
			continue
		}
		if fresh != nil {
			p.counts.replaced++
			p.mu.Unlock()
			logf("tifa: ssr: replaced growing VM: baseline=%d live=%d bound=%d fresh=%d bytes", old.baseline, old.live, old.memoryBound(p.memory), freshLive)
			fresh = nil
			p.closeVM(old)
			delay, warned = retryDelay, false
			continue
		}
		// The decision to load is serialized with queue admission. Requests arriving
		// after this point continue using the old VM while the load runs.
		p.mu.Unlock()
		var err error
		fresh, err = loadVM(p)
		if err == nil {
			continue
		}
		if !warned {
			logf("tifa: ssr: replace growing VM: %v, retrying with backoff", p.sources.explain(err, true))
			warned = true
		}
		timer := time.NewTimer(delay)
		select {
		case <-p.done:
			timer.Stop()
		case <-timer.C:
		}
		delay = min(2*delay, maxRetryDelay)
	}
}

// runGC runs a young GC on vm, then a full GC when the young GC left 1.5 times the live size or
// more. It runs only the full GC when vm ran fullGCEvery renders since its last full GC. With full it runs only the full GC: the engine collected during the render, and
// set its threshold from what the render held then. The full GC sets the live size and threshold
// from what is left, and the memory the VM holds free goes back to the system. runGC returns a Go panic, from the engine, as an error: in the goroutine of
// collect it has no caller to go up to, and would end the process.
func (p *Pool) runGC(vm *pooledVM, full bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	if p.collecting != nil {
		p.collecting(vm)
	}
	if !full && vm.young < fullGCEvery {
		vm.RunYoungGC()
		if vm.MemorySize() < vm.live+vm.live/2 {
			p.mu.Lock()
			p.counts.youngGCs++
			p.mu.Unlock()
			return nil
		}
	}
	vm.RunGC()
	vm.young = 0
	vm.live = vm.MemorySize()
	vm.setThreshold(p.memory)
	// What the VM holds free goes back to the system, as after Load. The renders until the next full GC
	// take what they need again, and a full GC is rare (see fullGCEvery). Keeping the room the
	// threshold leaves above the live size kept all of it, since that is more than a VM holds free.
	if _, err := vm.ReleaseMemory(0); err != nil {
		return err
	}
	p.mu.Lock()
	p.counts.idleGCs++
	p.mu.Unlock()
	return nil
}

// gcThreshold is the GC threshold of a VM for its next render, live being what its last GC between
// renders left, need what renders have shown they need (see pooledVM) and memory its limit. It is
// twice the live size, so that the garbage of several renders fits before a GC between renders,
// or need when that is more: a render that holds more than the live size until it ends, such as a
// large page, made the engine collect during it, and would again in each render. It is no more than
// half way to the limit, so that a render that makes more garbage than that still collects it
// before it runs out of memory.
func gcThreshold(live, need, memory uintptr) uintptr {
	return min(max(2*live, need), live/2+memory/2)
}

func (p *Pool) closeVM(vm *pooledVM) {
	err := vm.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts.closed++
	if err != nil {
		p.failure = errors.Join(p.failure, fmt.Errorf("ssr: close VM: %w", err))
	}
}

func (p *Pool) stats() poolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts
}
