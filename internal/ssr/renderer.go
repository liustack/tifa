package ssr

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liustack/quickjs"
	"github.com/liustack/quickjs/intl"
	"github.com/liustack/quickjs/temporal"
)

// Compile, Load and RenderVM are the steps a Pool takes, exported so that measurements outside
// the package can time and inspect each one. Code that serves pages uses a Pool.

// Compile turns the bundle into bytecode once, in a throwaway VM.
// Every VM of the pool, including replacements, loads this bytecode.
func Compile(source string, memory uintptr, timeout time.Duration) (bytecode []byte, err error) {
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

// Load creates a VM, gives it Intl and Temporal and runs the bundle's top level code, which
// defines startRender. Go callbacks provide formatting, dates and secure random bytes.
// Intl and Temporal use embedded zone data, and crypto reads the OS entropy source on demand.
// There is no module loader or application network or file I/O.
//
// Intl formats in en-US and UTC when a page passes no locale or timeZone, never in the host's
// language or zone. The local time of Date (getHours, toString, new Date(2026, 0, 1)) is UTC
// too: the engine's offset from UTC is always 0, and the zone of the process, which the app's
// Go code uses, is left alone. intl.Register only puts stand-ins on the VM: the first use of Intl or a
// toLocale* method installs the rest, so a VM whose pages never format costs a few KB more.
// dev runs intlcheck.js too, which installs Intl at once to watch it.
// Temporal is lazy too. Its default clock is time.Now, the live system clock Date.now uses,
// not a time frozen at VM creation or at the start of a render. Its default zone follows Intl.
//
// stack is how deep calls may nest, in the engine's stack slots (ssr.vm_stack_depth). Deeper
// calls throw RangeError: Maximum call stack size exceeded. The slots live on the goroutine
// stack of the render, not in the memory the limit counts: a slot takes about 1 KB.
func Load(bytecode []byte, memory, stack uintptr, timeout time.Duration, dev bool) (*quickjs.VM, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, err
	}
	vm.SetMemoryLimit(memory)
	vm.SetMaxStackSize(stack)
	if err = vm.SetEvalTimeout(timeout); err == nil {
		err = intl.Register(vm)
	}
	if err == nil {
		err = temporal.Register(vm)
	}
	if err == nil {
		err = registerRandom(vm)
	}
	if err == nil && dev {
		_, err = vm.Eval(intlCheck, quickjs.EvalGlobal)
	}
	if err == nil {
		_, err = vm.EvalBytecode(bytecode)
	}
	if err != nil {
		return nil, errors.Join(err, vm.Close())
	}
	// What loading left as garbage goes now, so that the live size of the VM is what the bundle
	// keeps, and the first render gets the threshold of the next ones.
	vm.RunGC()
	vm.SetGCThreshold(gcThreshold(vm.MemorySize(), 0, memory))
	return vm, nil
}

// intlCheck is what tifa dev runs on each VM to find the Intl calls that use the defaults.
//
//go:embed intlcheck.js
var intlCheck string

// RenderVM runs one request in vm: startRender(input), then the microtasks it queued,
// then reads globalThis.renderResult. Any error leaves vm in an unknown state.
func RenderVM(vm *quickjs.VM, input []byte, timeout time.Duration) (string, error) {
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
		if interrupted(err) || !time.Now().Before(deadline) {
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
		return "", &scriptError{text: result.Error}
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

// outOfMemory reports that the call ended because an allocation exceeded the VM's limit, also
// when the limit left no room for the error object. Page code cannot catch it: no catch, finally
// or promise handler runs, so the bundle never reports it as an error of its own. What page code
// throws never counts, whatever its name and message.
func outOfMemory(err error) bool {
	var e *quickjs.Error
	return errors.As(err, &e) && e.OutOfMemory
}

// interrupted reports that the eval timeout stopped the call. Nothing else interrupts a VM of the
// pool, so it is the time budget. What page code throws never counts, whatever its name and
// message.
func interrupted(err error) bool {
	var e *quickjs.Error
	return errors.As(err, &e) && e.Interrupted
}

func timedOut(timeout time.Duration, cause error) error {
	if cause == nil {
		return fmt.Errorf("ssr: render timed out after %s: %w", timeout, context.DeadlineExceeded)
	}
	return fmt.Errorf("ssr: render timed out after %s: %w (%w)", timeout, context.DeadlineExceeded, cause)
}
