package ssr

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liustack/slowjs"
	"github.com/liustack/slowjs/intl"
	"github.com/liustack/slowjs/temporal"
)

// Compile, Load and RenderVM are the steps a Pool takes, exported so that measurements outside
// the package can time and inspect each one. Code that serves pages uses a Pool.

// Compile turns the bundle, an ES module, into bytecode once, in a throwaway VM.
// Every VM of the pool, including replacements, loads this bytecode. The VMs share the source text
// of its functions, which their toString returns, and the tables error stacks read their lines
// from: these stay in the one copy of the bytecode, outside the memory of every VM and of
// ssr.vm_memory. The caller closes the bytecode, and its memory goes once the last VM that loaded
// it is closed too.
func Compile(source string, memory uintptr, timeout time.Duration) (bytecode *slowjs.SharedBytecode, err error) {
	vm, err := slowjs.NewVM()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, vm.Close()) }()
	vm.SetMemoryLimit(memory)
	if err := vm.SetEvalTimeout(timeout); err != nil {
		return nil, err
	}
	code, err := vm.Compile(source, slowjs.EvalModule)
	if err != nil {
		return nil, err
	}
	return slowjs.NewSharedBytecode(code)
}

// Check compiles the bundle as NewPool does, without running it, so that tifa build finds code
// the server could not compile, such as code only sloppy mode allows in a CommonJS file. The
// error gives the place in the sources by sourceMap, the source map of the bundle (nil without
// one). The memory limit and timeout are those of the build, not of a VM of the server: they
// only keep a broken compile from running away.
func Check(source string, sourceMap []byte) error {
	bytecode, err := Compile(source, checkMemory, checkTimeout)
	if err != nil {
		return fmt.Errorf("ssr: compile bundle: %w", (&sources{code: source, raw: sourceMap}).explain(err, true))
	}
	return bytecode.Close()
}

const (
	checkMemory  = 1 << 30
	checkTimeout = time.Minute
)

// Load creates a VM, gives it Intl and Temporal and runs the bundle's top level code, which
// defines startRender. A top-level await of the bundle finishes here: Load runs the jobs it
// waits for, and fails when it still waits once none is left, since a VM has no timers and does
// no I/O that could settle it. Go callbacks provide formatting, dates and secure random bytes.
// Intl and Temporal use embedded zone data, and crypto reads the OS entropy source on demand.
// There is no module loader or application network or file I/O.
//
// Intl formats in en-US and UTC when a page passes no locale or timeZone, never in the host's
// language or zone. The local time of Date (getHours, toString, new Date(2026, 0, 1)) is UTC
// too: the engine's offset from UTC is always 0, and the zone of the process, which the app's
// Go code uses, is left alone. intl.Register only puts stand-ins on the VM: the first use of Intl or a
// toLocale* method installs the rest, so a VM whose pages never format costs a few KB more.
// dev runs intlcheck.js too, which installs Intl at once to watch it.
// Temporal is lazy too. Date, Temporal and Intl share the VM clock, live during
// prewarming. Pool.Render may set a request clock after loading. Values already
// captured by module initialization stay unchanged. The default zone follows Intl.
//
// stack is how deep calls may nest, in the engine's stack slots (ssr.vm_stack_depth). Deeper
// calls throw RangeError: Maximum call stack size exceeded. The slots live on the goroutine
// stack of the render, not in the memory the limit counts: a slot takes about 1 KB.
//
// Load takes the functions RenderVM calls from the global object once, so that a render parses no
// code of its own: globalThis.startRender as the bundle left it, which a render calls even when
// the bundle replaces it later.
func Load(bytecode *slowjs.SharedBytecode, memory, stack uintptr, timeout time.Duration, dev bool) (*VM, error) {
	js, err := slowjs.NewVM()
	if err != nil {
		return nil, err
	}
	vm := &VM{VM: js}
	vm.SetMemoryLimit(memory)
	vm.SetMaxStackSize(stack)
	if err = vm.SetEvalTimeout(timeout); err == nil {
		err = intl.Register(vm.VM)
	}
	if err == nil {
		err = temporal.Register(vm.VM)
	}
	if err == nil {
		err = registerRandom(vm.VM)
	}
	if err == nil {
		err = registerHash(vm.VM)
	}
	if err == nil {
		err = registerEscape(vm.VM)
	}
	if err == nil {
		err = registerCopyProps(vm.VM)
	}
	if err == nil {
		err = registerAsyncContext(vm.VM)
	}
	if err == nil && dev {
		_, err = vm.Eval(intlCheck, slowjs.EvalGlobal)
	}
	if err == nil {
		_, err = vm.EvalSharedBytecode(bytecode)
	}
	if err == nil {
		err = vm.takeFunctions()
	}
	if err != nil {
		return nil, errors.Join(err, vm.VM.Close())
	}
	// What loading left as garbage goes now, so that the live size of the VM is what the bundle
	// keeps, and the first render gets the threshold of the next ones. The memory it took goes
	// back to the system: a VM of the pool may wait long for its first render, which takes it
	// again.
	vm.RunGC()
	vm.SetGCThreshold(gcThreshold(vm.MemorySize(), 0, memory))
	if _, err := vm.ReleaseMemory(0); err != nil {
		return nil, errors.Join(err, vm.Close())
	}
	return vm, nil
}

// intlCheck is what tifa dev runs on each VM to find the Intl calls that use the defaults.
//
//go:embed intlcheck.js
var intlCheck string

// VM is a VM of the bundle, made by Load, with the functions RenderVM calls.
type VM struct {
	*slowjs.VM
	// global is the global object, which startRender gets as this, as a call by its name would
	// give it. reset clears globalThis.renderResult, and readResult is the function of
	// readResult.
	global, reset, startRender, readResult slowjs.Value
}

// takeFunctions takes the functions RenderVM calls from the global object.
func (vm *VM) takeFunctions() error {
	values := [...]*slowjs.Value{&vm.global, &vm.reset, &vm.startRender, &vm.readResult}
	for i, source := range [...]string{"globalThis", resetResult, "globalThis.startRender", readResult} {
		v, err := vm.EvalValue(source, slowjs.EvalGlobal)
		if err != nil {
			for _, taken := range values[:i] {
				taken.Free()
			}
			return err
		}
		*values[i] = v
	}
	return nil
}

// Close frees the functions RenderVM calls and closes the VM.
func (vm *VM) Close() error {
	for _, v := range [...]*slowjs.Value{&vm.global, &vm.reset, &vm.startRender, &vm.readResult} {
		v.Free()
	}
	return vm.VM.Close()
}

// RenderVM runs one request in vm: startRender(input, ...texts), then the microtasks it queued,
// then reads globalThis.renderResult. Each of texts is an argument of startRender of its own. A render that left no result once no job is left waits for
// work nothing can settle, since a VM has no timers and does no I/O: when the bundle set
// globalThis.stopRender for it, RenderVM calls it, runs the jobs that queued and reads the
// result again. Any error leaves vm in an unknown state.
func RenderVM(vm *VM, input []byte, texts []string, timeout time.Duration) (string, error) {
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
	if _, err := vm.reset.Call(slowjs.UndefinedValue); err != nil {
		return "", phase("reset result", err)
	}
	if err := budget(); err != nil {
		return "", err
	}
	args := make([]any, 1+len(texts))
	args[0] = string(input)
	for i, text := range texts {
		args[1+i] = text
	}
	if _, err := vm.startRender.Call(vm.global, args...); err != nil {
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
	value, err := vm.readResult.Call(slowjs.UndefinedValue)
	if err != nil {
		return "", phase("read result", err)
	}
	if stopped, ok := value.(bool); ok && !stopped {
		if err := budget(); err != nil {
			return "", err
		}
		if _, err := vm.ExecutePendingJobs(); err != nil {
			return "", phase("microtasks", err)
		}
		if err := budget(); err != nil {
			return "", err
		}
		if value, err = vm.readResult.Call(slowjs.UndefinedValue); err != nil {
			return "", phase("read result", err)
		}
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

// resetResult clears globalThis.renderResult before a render.
const resetResult = `() => { globalThis.renderResult = undefined }`

// readResult takes globalThis.renderResult. Without one it calls globalThis.stopRender, when the
// bundle set it, and returns false.
const readResult = `() => {
  const result = globalThis.renderResult
  globalThis.renderResult = undefined
  if (result !== undefined || typeof globalThis.stopRender !== 'function') return result
  globalThis.stopRender()
  return false
}`

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
	var e *slowjs.Error
	return errors.As(err, &e) && e.OutOfMemory
}

// interrupted reports that the eval timeout stopped the call. Nothing else interrupts a VM of the
// pool, so it is the time budget. What page code throws never counts, whatever its name and
// message.
func interrupted(err error) bool {
	var e *slowjs.Error
	return errors.As(err, &e) && e.Interrupted
}

func timedOut(timeout time.Duration, cause error) error {
	if cause == nil {
		return fmt.Errorf("ssr: render timed out after %s: %w", timeout, context.DeadlineExceeded)
	}
	return fmt.Errorf("ssr: render timed out after %s: %w (%w)", timeout, context.DeadlineExceeded, cause)
}
