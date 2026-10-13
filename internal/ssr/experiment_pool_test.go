package ssr

// These lifecycle policies are experiments, not the HTTP server's default
// renderer or a guarantee of application state isolation.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-quick/internal/pages"
	"sync"
	"time"

	"modernc.org/quickjs"
)

type experimentConfig struct {
	Mode                     string
	Workers, Spares, MaxUses int
	Timeout                  time.Duration
	MemoryLimit              uintptr
	CollectEvery             int
}

type experimentStats struct {
	Created  int64 `json:"created"`
	Closed   int64 `json:"closed"`
	Live     int64 `json:"live"`
	PeakLive int64 `json:"peak_live"`
	Retired  int64 `json:"retired"`
	CreateNS int64 `json:"create_ns"`
	LoadNS   int64 `json:"load_ns"`
	CloseNS  int64 `json:"close_ns"`
}

type experimentVM struct {
	vm   *quickjs.VM
	uses int
}

type experimentRenderer struct {
	cfg             experimentConfig
	code            string
	bytecode        []byte
	compile         time.Duration
	slots           chan struct{}
	ready           chan *experimentVM
	ctx             context.Context
	cancel          context.CancelCauseFunc
	mu              sync.Mutex
	closing         bool
	failure         error
	counts          experimentStats
	active, refills sync.WaitGroup
	closeOnce       sync.Once
	closed          chan struct{}
}

var errExperimentClosed = errors.New("SSR experiment closed")

func newExperimentRenderer(code string, cfg experimentConfig) (*experimentRenderer, error) {
	if cfg.Workers < 1 || cfg.Timeout <= 0 || cfg.MaxUses < 0 || cfg.Spares < 0 {
		return nil, errors.New("invalid SSR experiment configuration")
	}
	switch cfg.Mode {
	case "source", "bytecode", "reuse":
		cfg.Spares = 0
	case "sync":
		cfg.Spares = 0
		if cfg.MaxUses == 0 {
			return nil, errors.New("recycling requires a positive MaxUses")
		}
	case "single":
		cfg.MaxUses = 1
	case "async":
		if cfg.MaxUses == 0 {
			return nil, errors.New("recycling requires a positive MaxUses")
		}
	default:
		return nil, fmt.Errorf("unknown experiment mode %q", cfg.Mode)
	}
	if cfg.MemoryLimit == 0 {
		cfg.MemoryLimit = 128 << 20
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	r := &experimentRenderer{cfg: cfg, code: code, ctx: ctx, cancel: cancel, slots: make(chan struct{}, cfg.Workers), closed: make(chan struct{})}
	if cfg.Mode != "source" {
		start := time.Now()
		vm, err := quickjs.NewVM()
		if err != nil {
			cancel(err)
			return nil, err
		}
		vm.SetMemoryLimit(cfg.MemoryLimit)
		err = vm.SetEvalTimeout(cfg.Timeout)
		if err == nil {
			r.bytecode, err = vm.Compile(code, quickjs.EvalGlobal)
		}
		err = errors.Join(err, vm.Close())
		r.compile = time.Since(start)
		if err != nil {
			cancel(err)
			return nil, err
		}
	}
	if cfg.Mode != "source" && cfg.Mode != "bytecode" {
		r.ready = make(chan *experimentVM, cfg.Workers+cfg.Spares)
		for range cap(r.ready) {
			vm, err := r.create()
			if err != nil {
				return nil, errors.Join(err, r.Close())
			}
			r.ready <- vm
		}
	}
	return r, nil
}

func (r *experimentRenderer) createWithContext(ctx context.Context) (*experimentVM, error) {
	start := time.Now()
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.counts.Created++
	r.counts.Live++
	r.counts.PeakLive = max(r.counts.PeakLive, r.counts.Live)
	r.counts.CreateNS += int64(time.Since(start))
	r.mu.Unlock()
	machine := &experimentVM{vm: vm}
	vm.SetMemoryLimit(r.cfg.MemoryLimit)
	start = time.Now()
	if err = setBudget(ctx, vm); err == nil {
		if r.bytecode == nil {
			_, err = vm.Eval(r.code, quickjs.EvalGlobal)
		} else {
			_, err = vm.EvalBytecode(r.bytecode)
		}
	}
	r.mu.Lock()
	r.counts.LoadNS += int64(time.Since(start))
	r.mu.Unlock()
	if err != nil {
		return nil, errors.Join(err, r.destroy(machine))
	}
	return machine, nil
}

func (r *experimentRenderer) create() (*experimentVM, error) {
	ctx, cancel := context.WithTimeout(r.ctx, r.cfg.Timeout)
	defer cancel()
	return r.createWithContext(ctx)
}

func (r *experimentRenderer) destroy(machine *experimentVM) error {
	start := time.Now()
	err := machine.vm.Close()
	r.mu.Lock()
	r.counts.Closed++
	r.counts.Live--
	r.counts.CloseNS += int64(time.Since(start))
	r.mu.Unlock()
	return err
}

func (r *experimentRenderer) fail(err error) {
	if err != nil {
		r.mu.Lock()
		r.failure = errors.Join(r.failure, err)
		r.mu.Unlock()
		r.cancel(err)
	}
}

// Destroy before constructing a replacement. Spares are fixed slots, so the
// background workers cannot grow the VM population beyond workers + spares.
func (r *experimentRenderer) replace(machine *experimentVM) error {
	if err := r.destroy(machine); err != nil {
		r.fail(err)
		return err
	}
	if r.ctx.Err() != nil {
		return nil
	}
	next, err := r.create()
	if err != nil {
		if r.ctx.Err() == nil {
			r.fail(err)
			return err
		}
		return nil
	}
	// Shutdown waits for replacements before draining this queue.
	r.ready <- next
	return nil
}

func (r *experimentRenderer) Render(ctx context.Context, snapshot pages.Snapshot) (result Result, err error) {
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return Result{}, errExperimentClosed
	}
	r.active.Add(1)
	r.mu.Unlock()
	defer r.active.Done()
	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-r.ctx.Done():
		return Result{}, context.Cause(r.ctx)
	}
	if r.ctx.Err() != nil {
		return Result{}, context.Cause(r.ctx)
	}
	input, err := json.Marshal(snapshot)
	if err != nil {
		return Result{}, err
	}
	if r.ready == nil {
		start := time.Now()
		machine, err := r.createWithContext(ctx)
		if err != nil {
			return Result{}, err
		}
		init := time.Since(start)
		result, err = renderVM(ctx, machine.vm, input)
		result.Init = init
		return result, errors.Join(err, r.destroy(machine))
	}
	var machine *experimentVM
	select {
	case machine = <-r.ready:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-r.ctx.Done():
		return Result{}, context.Cause(r.ctx)
	}
	if r.cfg.CollectEvery > 0 && machine.uses%r.cfg.CollectEvery == 0 {
		// Diagnostic only: the next object allocation triggers cycle collection.
		// This does not make reachable application caches collectible.
		machine.vm.SetGCThreshold(0)
	}
	result, err = renderVM(ctx, machine.vm, input)
	machine.uses++
	retire := err != nil || (r.cfg.Mode != "reuse" && machine.uses >= r.cfg.MaxUses)
	if retire {
		r.mu.Lock()
		r.counts.Retired++
		r.mu.Unlock()
		if r.cfg.Mode == "async" || r.cfg.Mode == "single" {
			r.refills.Go(func() { _ = r.replace(machine) })
		} else {
			err = errors.Join(err, r.replace(machine))
		}
	} else {
		r.ready <- machine
	}
	return result, err
}

func (r *experimentRenderer) stats() experimentStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts
}

func (r *experimentRenderer) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closing = true
		r.mu.Unlock()
		r.cancel(errExperimentClosed)
		r.active.Wait()
		r.refills.Wait()
		if r.ready != nil {
			for {
				select {
				case machine := <-r.ready:
					r.fail(r.destroy(machine))
				default:
					close(r.closed)
					return
				}
			}
		}
		close(r.closed)
	})
	<-r.closed
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failure
}
