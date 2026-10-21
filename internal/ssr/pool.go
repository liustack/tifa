// Package ssr renders pages to HTML in a pool of prewarmed QuickJS VMs.
//
// A bundle defines globalThis.startRender(json). It receives the JSON of Input
// and leaves globalThis.renderResult as a string: the HTML, which starts with <,
// on success, or the JSON of {"redirect": {location, status}} or {"error": ...}. Work may continue in microtasks, the
// host drains them before reading the result. Nothing else crosses the boundary.
package ssr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/liustack/quickjs"
	"github.com/liustack/tifa/internal/config"
)

// Input is everything a render sees. The HTML depends on nothing else.
type Input struct {
	BuildID string          `json:"buildId"`
	URL     string          `json:"url"`
	Data    json.RawMessage `json:"data"` // data injected by Load, already JSON
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
// The garbage renders leave is mostly collected between renders, after the answer
// (see release).
type Pool struct {
	bytecode []byte
	sources  *sources
	memory   uintptr
	stack    uintptr // stack slots of each VM, see Load
	timeout  time.Duration
	dev      bool // the checks of tifa dev, see warnIntl
	idle     chan *pooledVM
	done     chan struct{} // closed when Close starts

	mu      sync.Mutex
	closing bool
	failure error // errors from closing VMs, returned by Close
	counts  poolStats
	warned  map[string]bool // the Intl warnings logged

	renders   sync.WaitGroup
	refills   sync.WaitGroup
	closeOnce sync.Once

	acquired   func()          // test hook, runs after a render takes a VM
	collecting func(*pooledVM) // test hook, runs before a GC between renders
}

// pooledVM is a VM of the pool with what its GC threshold is made of (see gcThreshold).
type pooledVM struct {
	*quickjs.VM
	// live is the live size, what the VM held after its last GC between renders, or after Load.
	live uintptr
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

// renderGCs counts the renders in which the engine ran a GC, idleGCs the GCs between renders.
type poolStats struct{ created, closed, peak, renderGCs, idleGCs int }

// Replaced by tests.
var (
	logf       = log.Printf
	retryDelay = time.Second
	loadVM     = openVM
)

const maxRetryDelay = 30 * time.Second

// NewPool compiles the bundle source once and prewarms cfg.VMs VMs from the bytecode.
// It fails if the bundle does not compile or its top level code fails in any VM. sourceMap is
// the source map of the bundle (ssr.js.map), nil without one: the errors of the bundle's code
// give the places of their stack traces in the sources with it, in the bundle without it.
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
	}
	p := &Pool{
		sources: &sources{code: source, raw: sourceMap},
		memory:  uintptr(cfg.VMMemory),
		stack:   uintptr(cfg.VMStackDepth),
		timeout: cfg.Timeout,
		dev:     dev,
		idle:    make(chan *pooledVM, cfg.VMs),
		done:    make(chan struct{}),
		warned:  map[string]bool{},
	}
	var err error
	if p.bytecode, err = Compile(source, p.memory, p.timeout); err != nil {
		return nil, fmt.Errorf("ssr: compile bundle: %w", err)
	}
	for range cfg.VMs {
		vm, err := loadVM(p)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("ssr: prewarm VM: %w", p.sources.explain(err, true)), p.Close())
		}
		p.idle <- vm
	}
	return p, nil
}

// Render waits for an idle VM and renders in into a whole HTML document.
// While waiting it honours ctx. Once a VM is taken the render runs to completion
// or to the ssr.timeout budget, because stopping it halfway would cost the VM.
// A budget overrun returns an error that matches context.DeadlineExceeded. When the router
// redirects the URL, Render returns a *RedirectError.
func (p *Pool) Render(ctx context.Context, in Input) (string, error) {
	input, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("ssr: encode input: %w", err)
	}
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		return "", ErrClosed
	}
	p.renders.Add(1)
	p.mu.Unlock()
	defer p.renders.Done()

	var vm *pooledVM
	select {
	case vm = <-p.idle:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-p.done:
		return "", ErrClosed
	}
	// select picks at random among ready cases, so a canceled request or a closing
	// pool can still win the idle VM. Hand it back unused.
	if err := p.stopped(ctx); err != nil {
		p.idle <- vm
		return "", err
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
	html, err := RenderVM(vm.VM, input, p.timeout)
	var redirect *RedirectError
	if err != nil && !errors.As(err, &redirect) {
		return "", p.sources.explain(err, false)
	}
	if p.dev {
		// A warning must not fail the page. A VM that cannot list them is replaced.
		if warnErr := p.warnIntl(vm.VM); warnErr != nil {
			logf("tifa: ssr: read the Intl warnings: %v", warnErr)
			return html, err
		}
	}
	// A redirect is an answer, not a failure: the VM goes back to the pool.
	keep = true
	return html, err
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
			// The old VM is closed first, so the pool never holds more than cfg.VMs.
			p.idle <- vm
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
	return &pooledVM{VM: vm, live: vm.MemorySize(), threshold: vm.GCThreshold()}, nil
}

// release puts vm back in the pool after a render it survived. When its memory grew to 1.5 times
// its live size, where the engine would collect by itself, a goroutine collects it first, after
// the render has given its answer (see collect). A GC takes about as long as the garbage it frees,
// so collecting here takes it out of the renders, which with the threshold of gcThreshold rarely
// collect.
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
	// Under a limit below twice the live size the threshold is below 1.5 times the live size, so
	// a VM collected here is over it, and the next render would collect at its start.
	if vm.MemorySize() < vm.live+vm.live/2 {
		p.idle <- vm
		return
	}
	p.refills.Go(func() { p.collect(vm) })
}

// collect runs a GC between renders on vm and puts it back in the pool. Close waits for it, and
// once Close has started vm goes back as it is, for Close to close it. A GC that fails replaces vm.
func (p *Pool) collect(vm *pooledVM) {
	select {
	case <-p.done:
		p.idle <- vm
		return
	default:
	}
	if err := p.runGC(vm); err != nil {
		logf("tifa: ssr: GC between renders: %v, replacing the VM", err)
		p.closeVM(vm)
		p.refill()
		return
	}
	p.idle <- vm
}

// runGC collects vm and sets its live size and threshold from what is left. It returns a Go
// panic, from the engine, as an error: in the goroutine of collect it has no caller to go up to,
// and would end the process.
func (p *Pool) runGC(vm *pooledVM) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	if p.collecting != nil {
		p.collecting(vm)
	}
	vm.RunGC()
	vm.live = vm.MemorySize()
	vm.setThreshold(p.memory)
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
