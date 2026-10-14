package ssr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liustack/quickjs"
)

// compile turns the bundle into bytecode once, in a throwaway VM.
// Every VM of the pool, including replacements, loads this bytecode.
func compile(source string, memory uintptr, timeout time.Duration) (bytecode []byte, err error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, vm.Close()) }()
	vm.SetMemoryLimit(memory)
	if err := vm.SetEvalTimeout(timeout); err != nil {
		return nil, err
	}
	return vm.Compile(source, quickjs.EvalGlobal)
}

// load creates a VM and runs the bundle's top level code, which defines startRender.
// The VM gets no Go callbacks and no module loader, so rendering cannot do I/O.
func load(bytecode []byte, memory uintptr, timeout time.Duration) (*quickjs.VM, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, err
	}
	vm.SetMemoryLimit(memory)
	if err = vm.SetEvalTimeout(timeout); err == nil {
		_, err = vm.EvalBytecode(bytecode)
	}
	if err != nil {
		return nil, errors.Join(err, vm.Close())
	}
	return vm, nil
}

// render runs one request in vm: startRender(input), then the microtasks it queued,
// then reads globalThis.renderResult. Any error leaves vm in an unknown state.
func render(vm *quickjs.VM, input []byte, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	// Each phase gets only what is left of the request's budget.
	budget := func() error {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return timedOut(timeout, nil)
		}
		return vm.SetEvalTimeout(remaining)
	}
	phase := func(name string, err error) error {
		if err == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return timedOut(timeout, err)
		}
		if outOfMemory(err) {
			return fmt.Errorf("%w in %s (%w)", ErrOutOfMemory, name, err)
		}
		return fmt.Errorf("ssr: %s: %w", name, err)
	}
	if err := budget(); err != nil {
		return "", err
	}
	if _, err := vm.Eval("globalThis.renderResult = undefined", quickjs.EvalGlobal); err != nil {
		return "", phase("reset result", err)
	}
	if err := budget(); err != nil {
		return "", err
	}
	if _, err := vm.Call("startRender", string(input)); err != nil {
		return "", phase("startRender", err)
	}
	if err := budget(); err != nil {
		return "", err
	}
	if _, err := vm.ExecutePendingJobs(); err != nil {
		return "", phase("microtasks", err)
	}
	if err := budget(); err != nil {
		return "", err
	}
	value, err := vm.Eval("(() => { const result = globalThis.renderResult; globalThis.renderResult = undefined; return result })()", quickjs.EvalGlobal)
	if err != nil {
		return "", phase("read result", err)
	}
	text, ok := value.(string)
	if !ok {
		return "", errors.New("ssr: render left unresolved work after draining microtasks")
	}
	// A page is the HTML itself, so it crosses the boundary without a JSON round trip. Anything
	// else is JSON, which starts with {.
	if strings.HasPrefix(text, "<") {
		return text, nil
	}
	var result struct {
		Redirect *RedirectError `json:"redirect"`
		Error    string         `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return "", fmt.Errorf("ssr: decode result: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("ssr: render page: %s", result.Error)
	}
	if result.Redirect != nil {
		return "", result.Redirect
	}
	return "", errors.New("ssr: render returned neither HTML nor a redirect or an error")
}

// RedirectError is what Render returns when the router answers the URL with a redirect, as
// TanStack Router does for a URL it spells differently (/%61 for /a). The VM is fine.
type RedirectError struct {
	Location string `json:"location"`
	Status   int    `json:"status"`
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("ssr: the router redirects to %s (%d)", e.Location, e.Status)
}

// ErrOutOfMemory matches a render that hit the VM's memory limit (ssr.vm_memory).
var ErrOutOfMemory = errors.New("ssr: VM out of memory")

// outOfMemory reports the exception QuickJS throws when an allocation exceeds the limit.
// When the limit leaves no room even for that error object, QuickJS throws null instead,
// which cannot be told apart from application code throwing null. Such a render still
// fails and retires its VM, it just does not match ErrOutOfMemory.
func outOfMemory(err error) bool {
	var e *quickjs.Error
	return errors.As(err, &e) && e.Name == "InternalError" && e.Message == "out of memory"
}

func timedOut(timeout time.Duration, cause error) error {
	if cause == nil {
		return fmt.Errorf("ssr: render timed out after %s: %w", timeout, context.DeadlineExceeded)
	}
	return fmt.Errorf("ssr: render timed out after %s: %w (%w)", timeout, context.DeadlineExceeded, cause)
}
