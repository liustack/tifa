// Package ssr renders pages to HTML in a pool of prewarmed QuickJS VMs.
//
// A bundle defines globalThis.startRender(json). It receives the JSON of Input
// and leaves globalThis.renderResult as a JSON string, {"html": ...} on success
// or {"error": ...} when the page failed. Work may continue in microtasks, the
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
}

// ErrClosed is returned by Render after Close has started.
var ErrClosed = errors.New("ssr: pool closed")

// Pool holds cfg.VMs prewarmed VMs. A VM runs one render at a time and stays
// in the pool for the next one. A VM whose render fails for any reason (a JS
// exception, the time budget, the memory limit) leaves the pool, and a
// background goroutine replaces it with a fresh VM loaded from the same bytecode.
type Pool struct {
	bytecode []byte
	memory   uintptr
	timeout  time.Duration
	idle     chan *quickjs.VM
	done     chan struct{} // closed when Close starts

	mu      sync.Mutex
	closing bool
	failure error // errors from closing VMs, returned by Close
	counts  poolStats

	renders   sync.WaitGroup
	refills   sync.WaitGroup
	closeOnce sync.Once

	acquired func() // test hook, runs after a render takes a VM
}

type poolStats struct{ created, closed, peak int }

// Replaced by tests.
var (
	logf       = log.Printf
	retryDelay = time.Second
	loadVM     = openVM
)

const maxRetryDelay = 30 * time.Second

// NewPool compiles the bundle source once and prewarms cfg.VMs VMs from the bytecode.
// It fails if the bundle does not compile or its top level code fails in any VM.
func NewPool(source string, cfg config.SSR) (*Pool, error) {
	switch {
	case cfg.VMs < 1:
		return nil, fmt.Errorf("ssr: ssr.vms must be at least 1, got %d", cfg.VMs)
	case cfg.VMMemory <= 0:
		return nil, fmt.Errorf("ssr: ssr.vm_memory must be positive, got %d", cfg.VMMemory)
	case cfg.Timeout <= 0:
		return nil, fmt.Errorf("ssr: ssr.timeout must be positive, got %s", cfg.Timeout)
	}
	p := &Pool{
		memory:  uintptr(cfg.VMMemory),
		timeout: cfg.Timeout,
		idle:    make(chan *quickjs.VM, cfg.VMs),
		done:    make(chan struct{}),
	}
	var err error
	if p.bytecode, err = compile(source, p.memory, p.timeout); err != nil {
		return nil, fmt.Errorf("ssr: compile bundle: %w", err)
	}
	for range cfg.VMs {
		vm, err := loadVM(p)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("ssr: prewarm VM: %w", err), p.Close())
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

	var vm *quickjs.VM
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
	if p.acquired != nil {
		p.acquired()
	}
	html, err := render(vm, input, p.timeout)
	var redirect *RedirectError
	if err != nil && !errors.As(err, &redirect) {
		p.retire(vm)
		return "", err
	}
	// A redirect is an answer, not a failure: the VM goes back to the pool.
	p.idle <- vm
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
func (p *Pool) retire(vm *quickjs.VM) {
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
		logf("tifa: ssr: replace VM: %v, retrying in %s", err, delay)
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

func openVM(p *Pool) (*quickjs.VM, error) {
	vm, err := load(p.bytecode, p.memory, p.timeout)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.counts.created++
	p.counts.peak = max(p.counts.peak, p.counts.created-p.counts.closed)
	p.mu.Unlock()
	return vm, nil
}

func (p *Pool) closeVM(vm *quickjs.VM) {
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
