package ssr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/liustack/tifa/internal/pages"
	"modernc.org/quickjs"
	"time"
)

type Result struct {
	HTML   string
	Init   time.Duration
	Render time.Duration
}
type Renderer struct {
	code    string
	slots   chan struct{}
	timeout time.Duration
}

func New(bundle *Bundle, concurrency int, timeout time.Duration) *Renderer {
	if concurrency < 1 || timeout <= 0 {
		panic("SSR requires positive concurrency and timeout")
	}
	return &Renderer{code: bundle.code, slots: make(chan struct{}, concurrency), timeout: timeout}
}

func (r *Renderer) Render(ctx context.Context, snapshot pages.Snapshot) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	input, err := json.Marshal(snapshot)
	if err != nil {
		return Result{}, err
	}
	start := time.Now()
	vm, err := quickjs.NewVM()
	if err != nil {
		return Result{}, err
	}
	defer vm.Close()
	vm.SetMemoryLimit(128 << 20)
	// Give each execution phase only the budget remaining for the whole request.
	// No Go callbacks or default module loader are registered in this VM.
	if err = setBudget(ctx, vm); err != nil {
		return Result{}, err
	}
	if _, err = vm.Eval(r.code, quickjs.EvalGlobal); err != nil {
		return Result{}, fmt.Errorf("initialize SSR: %w", err)
	}
	init := time.Since(start)
	result, err := renderVM(ctx, vm, input)
	result.Init = init
	return result, err
}

func setBudget(ctx context.Context, vm *quickjs.VM) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("SSR requires an execution deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	return vm.SetEvalTimeout(remaining)
}

// Clearing the result releases the previous HTML, not arbitrary application state.
// The fresh renderer and the reuse experiment share this request boundary.
func renderVM(ctx context.Context, vm *quickjs.VM, input []byte) (Result, error) {
	renderStart := time.Now()
	if err := setBudget(ctx, vm); err != nil {
		return Result{}, err
	}
	if _, err := vm.Eval("globalThis.renderResult = undefined", quickjs.EvalGlobal); err != nil {
		return Result{}, err
	}
	if err := setBudget(ctx, vm); err != nil {
		return Result{}, err
	}
	if _, err := vm.Call("startRender", string(input)); err != nil {
		return Result{}, fmt.Errorf("start SSR: %w", err)
	}
	if err := setBudget(ctx, vm); err != nil {
		return Result{}, err
	}
	if _, err := vm.ExecutePendingJobs(); err != nil {
		return Result{}, fmt.Errorf("SSR microtasks: %w", err)
	}
	if err := setBudget(ctx, vm); err != nil {
		return Result{}, err
	}
	value, err := vm.Eval("(() => { const result = globalThis.renderResult; globalThis.renderResult = undefined; return result; })()", quickjs.EvalGlobal)
	if err != nil {
		return Result{}, err
	}
	jsonResult, ok := value.(string)
	if !ok {
		return Result{}, errors.New("SSR has unresolved work after draining pure microtasks")
	}
	var result struct {
		HTML  string `json:"html"`
		Error string `json:"error"`
	}
	if err = json.Unmarshal([]byte(jsonResult), &result); err != nil {
		return Result{}, err
	}
	if result.Error != "" {
		return Result{}, fmt.Errorf("render page: %s", result.Error)
	}
	if result.HTML == "" {
		return Result{}, errors.New("SSR returned no HTML")
	}
	return Result{HTML: result.HTML, Render: time.Since(renderStart)}, nil
}
