package ssr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	lib "github.com/liustack/quickjs/lib"
	"github.com/liustack/tifa/internal/config"
)

func testSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/app.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func testConfig(vms int) config.SSR {
	return config.SSR{VMs: vms, VMMemory: 8 << 20, VMStackDepth: config.Default().SSR.VMStackDepth, Timeout: 2 * time.Second, QueueLimit: 32, WaitTimeout: time.Second}
}

// modeConfig is the config of a pool for a render of data.mode: a short time budget for the modes
// that run until it ends, the default of testConfig for the others, since filling the memory or
// the stack takes longer than a short budget under the race detector.
func modeConfig(mode string) config.SSR {
	cfg := testConfig(1)
	if mode == "loop" || mode == "jobs" || mode == "loopcaught" {
		cfg.Timeout = 100 * time.Millisecond
	}
	return cfg
}

func newTestPool(t *testing.T, source string, cfg config.SSR) *Pool {
	t.Helper()
	p, err := NewPool(source, nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func input(mode, text string) Input {
	data, err := json.Marshal(map[string]string{"mode": mode, "text": text})
	if err != nil {
		panic(err)
	}
	return Input{BuildID: "b1", URL: "/orders/1", Data: data}
}

func TestRenderReturnsHTML(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	html, err := p.Render(context.Background(), input("ok", "hello"))
	if err != nil {
		t.Fatal(err)
	}
	want := `<main data-build="b1" data-url="/orders/1" data-renders="1" data-dirty="false">hello</main>`
	if html != want {
		t.Fatalf("got %s, want %s", html, want)
	}
}

func TestPoolPrewarmsVMsAndCompilesOnce(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(3))
	s := p.stats()
	if s.created != 3 || s.closed != 0 {
		t.Fatalf("want 3 prewarmed VMs, got %+v", s)
	}
	if len(p.bytecode) == 0 {
		t.Fatal("bytecode was not compiled")
	}
}

func TestNewPoolRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		cfg    config.SSR
		want   string
	}{
		{"no VMs", "", config.SSR{VMs: 0, VMMemory: 1 << 20, VMStackDepth: 1000, Timeout: time.Second, QueueLimit: 32, WaitTimeout: time.Second}, "ssr.vms"},
		{"no memory", "", config.SSR{VMs: 1, VMMemory: 0, VMStackDepth: 1000, Timeout: time.Second, QueueLimit: 32, WaitTimeout: time.Second}, "ssr.vm_memory"},
		{"no stack", "", config.SSR{VMs: 1, VMMemory: 1 << 20, VMStackDepth: 0, Timeout: time.Second, QueueLimit: 32, WaitTimeout: time.Second}, "ssr.vm_stack_depth"},
		{"deep stack", "", config.SSR{VMs: 1, VMMemory: 1 << 20, VMStackDepth: lib.MaxStackSlots + 1, Timeout: time.Second, QueueLimit: 32, WaitTimeout: time.Second}, "ssr.vm_stack_depth"},
		{"no timeout", "", config.SSR{VMs: 1, VMMemory: 1 << 20, VMStackDepth: 1000, Timeout: 0, QueueLimit: 32, WaitTimeout: time.Second}, "ssr.timeout"},
		{"syntax", "globalThis.startRender = (", testConfig(1), "compile"},
		{"top level throws", "throw new Error('broken bundle')", testConfig(1), "broken bundle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPool(tc.source, nil, tc.cfg, false)
			if err == nil {
				p.Close()
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// The router redirects a URL it spells differently, such as /%61 for /a. That is no failure,
// so the VM stays in the pool.
func TestRedirectKeepsTheVM(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	_, err := p.Render(context.Background(), input("redirect", ""))
	var redirect *RedirectError
	if !errors.As(err, &redirect) || redirect.Location != "/a?q=1" || redirect.Status != 307 {
		t.Fatalf("got %v", err)
	}
	html, err := p.Render(context.Background(), input("ok", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `data-renders="2"`) {
		t.Fatalf("the redirect retired the VM: %s", html)
	}
}

func TestVMIsReusedAcrossRenders(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	for i := 1; i <= 3; i++ {
		html, err := p.Render(context.Background(), input("ok", ""))
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf(`data-renders="%d"`, i); !strings.Contains(html, want) {
			t.Fatalf("render %d: got %s", i, html)
		}
	}
	if s := p.stats(); s.created != 1 {
		t.Fatalf("want one reused VM, got %+v", s)
	}
}

func TestVMExposesNoIO(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	html, err := p.Render(context.Background(), input("globals", ""))
	if err != nil {
		t.Fatal(err)
	}
	if html != "<pre>[]</pre>" {
		t.Fatalf("unexpected host capabilities: %s", html)
	}
}

func TestInvalidInputFailsBeforeJS(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	_, err := p.Render(context.Background(), Input{Data: json.RawMessage(`{"broken"`)})
	if err == nil || !strings.Contains(err.Error(), "encode input") {
		t.Fatalf("got %v", err)
	}
	if s := p.stats(); s.created != 1 || s.closed != 0 {
		t.Fatalf("an input error retired a VM: %+v", s)
	}
}

// Every failure retires the VM. The next render must run in a fresh VM,
// so it sees renders == 1 and no state left by the failed request.
func TestFailedVMIsReplaced(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"throw", "intentional failure"},
		{"error", "page failed"},
		{"pending", "unresolved"},
		{"loop", "timed out"},
		{"jobs", "timed out"},
		{"oom", "out of memory"},
		{"oomsmall", "out of memory"},
		{"oomcaught", "out of memory"},
		{"loopcaught", "timed out"},
		{"recurse", "RangeError: Maximum call stack size exceeded"},
		{"recursecaught", "RangeError: Maximum call stack size exceeded"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			p := newTestPool(t, testSource(t), modeConfig(tc.mode))
			before := p.stats()
			_, err := p.Render(context.Background(), input(tc.mode, ""))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			html, err := p.Render(context.Background(), input("ok", ""))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(html, `data-renders="1" data-dirty="false"`) {
				t.Fatalf("failed VM was reused: %s", html)
			}
			after := p.stats()
			if after.created != before.created+1 || after.closed != before.closed+1 {
				t.Fatalf("want one VM replaced, before %+v after %+v", before, after)
			}
		})
	}
}

// A Go panic during a render goes on up to the caller, and the VM it left in an unknown state
// is replaced, so the pool keeps its size.
func TestPanickedVMIsReplaced(t *testing.T) {
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		vm, err := openVM(p)
		if err != nil {
			return nil, err
		}
		return vm, vm.RegisterFunc("goPanic", func() { panic("go panic in a render") }, false)
	})()
	p := newTestPool(t, testSource(t), testConfig(1))
	func() {
		defer func() {
			if r := recover(); r != "go panic in a render" {
				t.Errorf("recovered %v, want the panic of the render", r)
			}
		}()
		_, _ = p.Render(context.Background(), input("gopanic", ""))
		t.Error("Render returned")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	html, err := p.Render(ctx, input("ok", ""))
	if err != nil {
		t.Fatalf("the next render: %v", err)
	}
	if !strings.Contains(html, `data-renders="1" data-dirty="false"`) {
		t.Fatalf("the VM of the panic was reused: %s", html)
	}
	if s := p.stats(); s.created != 2 || s.closed != 1 {
		t.Fatalf("want one VM replaced, got %+v", s)
	}
}

func TestTimeoutIsDeadlineExceeded(t *testing.T) {
	cfg := testConfig(1)
	cfg.Timeout = 50 * time.Millisecond
	p := newTestPool(t, testSource(t), cfg)
	start := time.Now()
	_, err := p.Render(context.Background(), input("loop", ""))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("budget not enforced, took %s", elapsed)
	}
}

func TestConcurrentRendersSeeOwnData(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(2))
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			name := fmt.Sprintf("request-%d", i)
			html, err := p.Render(context.Background(), input("ok", name))
			if err != nil {
				t.Error(err)
				return
			}
			if !strings.HasSuffix(html, ">"+name+"</main>") {
				t.Errorf("want %s, got %s", name, html)
			}
		})
	}
	wg.Wait()
	if s := p.stats(); s.created != 2 || s.peak != 2 {
		t.Fatalf("pool grew or shrank: %+v", s)
	}
}

// holdVMs makes every render wait inside the pool, with its VM taken,
// until the test releases it. It reports each acquisition on the returned channel.
func holdVMs(p *Pool) (acquired chan struct{}, release func()) {
	acquired = make(chan struct{}, 64)
	gate := make(chan struct{})
	p.acquired = func() {
		acquired <- struct{}{}
		<-gate
	}
	var once sync.Once
	return acquired, func() { once.Do(func() { close(gate) }) }
}

func TestQueuedRenderHonoursCancel(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	acquired, release := holdVMs(p)
	defer release()
	first := make(chan error, 1)
	go func() {
		_, err := p.Render(context.Background(), input("ok", "first"))
		first <- err
	}()
	<-acquired
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() {
		_, err := p.Render(ctx, input("ok", "second"))
		queued <- err
	}()
	cancel()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued render: got %v", err)
	}
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.created != 1 || s.closed != 0 {
		t.Fatalf("cancel touched a VM: %+v", s)
	}
}

func TestCloseWaitsForRendersAndReleasesVMs(t *testing.T) {
	p, err := NewPool(testSource(t), nil, testConfig(2), false)
	if err != nil {
		t.Fatal(err)
	}
	acquired, release := holdVMs(p)
	results := make(chan error, 3)
	for i := range 3 {
		go func() {
			_, err := p.Render(context.Background(), input("ok", fmt.Sprint(i)))
			results <- err
		}()
	}
	<-acquired
	<-acquired
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	// The third render waits for a VM. Close must wake it with ErrClosed.
	if err := <-results; !errors.Is(err, ErrClosed) {
		t.Fatalf("queued render: got %v", err)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned with renders in flight: %v", err)
	default:
	}
	release()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("in-flight render: %v", err)
		}
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.created != s.closed || s.created != 2 {
		t.Fatalf("VMs left open: %+v", s)
	}
	if _, err := p.Render(context.Background(), input("ok", "")); !errors.Is(err, ErrClosed) {
		t.Fatalf("render after close: got %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseWaitsForReplacement(t *testing.T) {
	cfg := testConfig(1)
	p, err := NewPool(testSource(t), nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Render(context.Background(), input("throw", "")); err == nil {
		t.Fatal("accepted")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.created != s.closed {
		t.Fatalf("replacement VM left open: %+v", s)
	}
}

// A failed replacement is logged and retried, so the pool does not lose a VM silently.
func TestFailedReplacementIsLoggedAndRetried(t *testing.T) {
	logs := make(chan string, 16)
	defer swap(&logf, func(format string, args ...any) { logs <- fmt.Sprintf(format, args...) })()
	defer swap(&retryDelay, time.Millisecond)()
	p := newTestPool(t, testSource(t), testConfig(1))
	failures := 2
	var mu sync.Mutex
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		mu.Lock()
		defer mu.Unlock()
		if failures > 0 {
			failures--
			return nil, errors.New("no memory for a VM")
		}
		return openVM(p)
	})()
	if _, err := p.Render(context.Background(), input("throw", "")); err == nil {
		t.Fatal("accepted")
	}
	for range 2 {
		if msg := <-logs; !strings.Contains(msg, "no memory for a VM") {
			t.Fatalf("unexpected log %q", msg)
		}
	}
	html, err := p.Render(context.Background(), input("ok", ""))
	if err != nil || !strings.Contains(html, `data-renders="1"`) {
		t.Fatalf("pool did not recover: %s, %v", html, err)
	}
}

// Close stops a replacement that keeps failing instead of waiting for it forever.
func TestCloseStopsRetryingReplacement(t *testing.T) {
	logs := make(chan string, 16)
	defer swap(&logf, func(format string, args ...any) {
		select {
		case logs <- fmt.Sprintf(format, args...):
		default:
		}
	})()
	defer swap(&retryDelay, time.Hour)()
	p, err := NewPool(testSource(t), nil, testConfig(1), false)
	if err != nil {
		t.Fatal(err)
	}
	defer swap(&loadVM, func(*Pool) (*pooledVM, error) { return nil, errors.New("broken") })()
	if _, err := p.Render(context.Background(), input("throw", "")); err == nil {
		t.Fatal("accepted")
	}
	<-logs
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if s := p.stats(); s.created != s.closed {
		t.Fatalf("VMs left open: %+v", s)
	}
}

func swap[T any](target *T, value T) (restore func()) {
	old := *target
	*target = value
	return func() { *target = old }
}

// A canceled request must not render even when an idle VM is ready at the same time.
// The two select cases are both ready here, so without a check after taking the VM
// about half of these calls would render.
func TestCanceledRequestDoesNotRender(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 50 {
		if _, err := p.Render(ctx, input("ok", "")); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	}
	html, err := p.Render(context.Background(), input("ok", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `data-renders="1"`) {
		t.Fatalf("a canceled request rendered: %s", html)
	}
	if s := p.stats(); s.created != 1 || s.closed != 0 {
		t.Fatalf("cancel touched a VM: %+v", s)
	}
}

// A render admitted just before Close must not start once Close has begun.
// The test recreates that window by closing done without setting closing.
func TestRenderAdmittedBeforeCloseDoesNotStart(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	ran := 0
	p.acquired = func() { ran++ }
	close(p.done)
	defer func() { p.done = make(chan struct{}) }() // let the cleanup Close run normally
	for range 50 {
		if _, err := p.Render(context.Background(), input("ok", "")); !errors.Is(err, ErrClosed) {
			t.Fatalf("got %v", err)
		}
	}
	if ran != 0 {
		t.Fatalf("%d renders started after Close began", ran)
	}
	if len(p.idle) != 1 {
		t.Fatalf("the VM was not returned to the pool, %d idle", len(p.idle))
	}
}

// Only the memory limit matches ErrOutOfMemory. Other failures must not, so callers
// and tests can tell a VM that ran out of memory from one that failed otherwise.
func TestOutOfMemoryIsDistinct(t *testing.T) {
	for _, tc := range []struct {
		mode string
		oom  bool
	}{
		{"oom", true},
		{"oomsmall", true},
		{"oomcaught", true},
		{"throw", false},
		{"error", false},
		{"pending", false},
		{"loop", false},
		{"loopcaught", false},
		{"recurse", false},
	} {
		p := newTestPool(t, testSource(t), modeConfig(tc.mode))
		_, err := p.Render(context.Background(), input(tc.mode, ""))
		if err == nil {
			t.Fatalf("%s: accepted", tc.mode)
		}
		if got := errors.Is(err, ErrOutOfMemory); got != tc.oom {
			t.Errorf("%s: errors.Is(%v, ErrOutOfMemory) = %v", tc.mode, err, got)
		}
	}
}

// Page code cannot catch running out of memory or the time budget: a catch that reports every
// error as the page's, as the runtime's startRender does, never runs, and the render fails with
// ErrOutOfMemory or context.DeadlineExceeded.
func TestPageCodeCannotCatchOutOfMemoryOrTimeout(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want error
	}{
		{"oomcaught", ErrOutOfMemory},
		{"loopcaught", context.DeadlineExceeded},
	} {
		p := newTestPool(t, testSource(t), modeConfig(tc.mode))
		_, err := p.Render(context.Background(), input(tc.mode, ""))
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.mode, err, tc.want)
		}
		var reported *scriptError
		if errors.As(err, &reported) {
			t.Errorf("%s: the page caught it: %v", tc.mode, err)
		}
	}
}

// An error page code throws is the page's, even when its name and message are those of the
// engine's interrupt or of running out of memory: only the engine can end a call so that no page
// code catches it.
func TestThrownLookalikesAreErrorsOfThePage(t *testing.T) {
	for _, mode := range []string{"fakeinterrupt", "fakeoom", "fakeoomobject"} {
		p := newTestPool(t, testSource(t), testConfig(1))
		_, err := p.Render(context.Background(), input(mode, ""))
		if err == nil {
			t.Fatalf("%s: accepted", mode)
		}
		if errors.Is(err, ErrOutOfMemory) || errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: got %v, want an error of the page", mode, err)
		}
		if !strings.HasPrefix(err.Error(), "ssr: startRender: ") {
			t.Errorf("%s: got %v, want the thrown value", mode, err)
		}
	}
}

// A component that recurses without end fails its render with the RangeError of a JavaScript
// engine, whether the page catches it or not, before the time budget. The process lives on, and
// the VM is replaced as after any failed render.
func TestInfiniteRecursionIsARenderError(t *testing.T) {
	p := newTestPool(t, testSource(t), testConfig(1))
	for _, mode := range []string{"recurse", "recursecaught"} {
		_, err := p.Render(context.Background(), input(mode, ""))
		if err == nil || !strings.Contains(err.Error(), "RangeError: Maximum call stack size exceeded") {
			t.Fatalf("%s: got %v", mode, err)
		}
		if errors.Is(err, ErrOutOfMemory) || errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: got %v, want an error of the page", mode, err)
		}
		html, err := p.Render(context.Background(), input("ok", ""))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, `data-renders="1" data-dirty="false"`) {
			t.Fatalf("%s: the VM was reused: %s", mode, html)
		}
	}
}

// tifa's default and largest stack depth is the engine's default, which recurses about as deep as
// Node does.
func TestDefaultStackDepthIsTheEngines(t *testing.T) {
	if got := config.Default().SSR.VMStackDepth; got != lib.MaxStackSlots {
		t.Fatalf("ssr.vm_stack_depth defaults to %d, the engine to %d", got, lib.MaxStackSlots)
	}
	if config.MaxVMStackDepth != lib.MaxStackSlots {
		t.Fatalf("ssr.vm_stack_depth may go up to %d, the engine's default is %d", config.MaxVMStackDepth, lib.MaxStackSlots)
	}
}

// ssr.vm_stack_depth is how deep calls nest in a VM before they throw the RangeError, also in
// a replacement VM.
func TestStackDepthLimitsRecursion(t *testing.T) {
	depth := regexp.MustCompile(`<p data-depth="(\d+)">RangeError: Maximum call stack size exceeded</p>`)
	for _, tc := range []struct{ limit, least int }{{500, 450}, {config.Default().SSR.VMStackDepth, 9900}} {
		cfg := testConfig(1)
		cfg.VMStackDepth = tc.limit
		p := newTestPool(t, testSource(t), cfg)
		for _, mode := range []string{"depth", "recurse", "depth"} {
			html, err := p.Render(context.Background(), input(mode, ""))
			if mode == "recurse" {
				if err == nil || !strings.Contains(err.Error(), "RangeError: Maximum call stack size exceeded") {
					t.Fatalf("%d: recurse: %v", tc.limit, err)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			m := depth.FindStringSubmatch(html)
			if m == nil {
				t.Fatalf("%d: got %s", tc.limit, html)
			}
			if n, _ := strconv.Atoi(m[1]); n < tc.least || n > tc.limit {
				t.Errorf("%d: calls nested %d deep, want %d to %d", tc.limit, n, tc.least, tc.limit)
			}
		}
		if s := p.stats(); s.created != 2 {
			t.Fatalf("%d: want the VM of recurse replaced, got %+v", tc.limit, s)
		}
	}
}

// The pool never interrupts a VM itself, so an interrupt is the time budget whatever Go's clock
// says. The page interrupts its VM long before the deadline, as the engine's timer would.
func TestInterruptIsTheTimeBudget(t *testing.T) {
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		vm, err := openVM(p)
		if err != nil {
			return nil, err
		}
		return vm, vm.RegisterFunc("goInterrupt", vm.Interrupt, false)
	})()
	p := newTestPool(t, testSource(t), testConfig(1))
	_, err := p.Render(context.Background(), input("interrupt", ""))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}
