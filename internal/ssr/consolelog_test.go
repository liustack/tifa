package ssr

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// consoleBundle is a page that calls console on line 1, which the source map names app/page.tsx,
// and the host script after it. The mode of the data picks what the page calls.
const consoleBundle = `globalThis.startRender = function (json) { const mode = JSON.parse(json).data.mode; globalThis.renderResult = '<p>' + String(run(mode)) + '</p>' }; function run(mode) { if (mode === 'log') { console.log('hello', { a: 1 }, [1, 'x'], null, undefined, 3n); console.info('%s has %d items 100%%', 'cart', 3, 'extra'); console.debug('quiet'); console.warn('careful'); return 'done' } if (mode === 'error') { console.log('before'); console.error('broken', 1); return 'unreachable' } if (mode === 'assert') { console.assert(false, 'bad'); return 'unreachable' } if (mode === 'more') { console.table([{ a: 1 }]); console.dir({ b: 2 }, { depth: 0 }); console.group('section'); console.groupCollapsed(); console.groupEnd(); console.time('t'); console.timeLog('t'); console.timeEnd('t'); console.count('c'); console.countReset('c'); console.trace('where'); return 'done' } if (mode === 'throw') { console.log('then'); throw new Error('boom') } if (mode === 'long') { for (let i = 0; i < 500; i++) console.log('line ' + i); console.log('x'.repeat(10000)); return 'done' } if (mode === 'bad') { const o = { get x() { throw new Error('getter') } }; console.log('a', o); const c = {}; c.self = c; console.log(c); return 'done' } return 'ok' }
`

func consolePool(t *testing.T, dev bool) *Pool {
	t.Helper()
	host, err := os.ReadFile("../../js/runtime/host.js")
	if err != nil {
		t.Fatal(err)
	}
	src := consoleBundle + string(host)
	p, err := NewPool(src, sourceMapOf(mapping{0, 0, "app/page.tsx", 11, 4}), testConfig(1), dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func renderMode(ctx context.Context, p *Pool, url, mode string) (string, error) {
	return p.Render(ctx, Input{BuildID: "b1", URL: url, Data: []byte(`{"mode":"` + mode + `"}`)})
}

func logged(t *testing.T) (*[]string, func()) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	restore := captureLogs(func(s string) {
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	})
	return &got, restore
}

// Under tifa dev, log, info, debug and warn of a render reach the log of the app, each with the
// path of the page and the place of the call in the sources. The page renders as it does without.
func TestDevConsoleWritesToTheLog(t *testing.T) {
	p := consolePool(t, true)
	got, restore := logged(t)
	defer restore()
	html, err := renderMode(context.Background(), p, "/projects/studio?tab=2", "log")
	if err != nil {
		t.Fatal(err)
	}
	if html != "<p>done</p>" {
		t.Errorf("html %s", html)
	}
	at := " path=/projects/studio?tab=2 at=app/page.tsx:12:5"
	want := []string{
		`console.log: hello {"a":1} [1,"x"] null undefined 3` + at,
		`console.info: cart has 3 items 100% extra` + at,
		`console.debug: quiet` + at,
		`console.warn: careful` + at,
	}
	if !slices.Equal(*got, want) {
		t.Errorf("logged\n%s\nwant\n%s", strings.Join(*got, "\n"), strings.Join(want, "\n"))
	}
}

// A built app prints nothing, and the calls do nothing.
func TestBuiltConsoleIsSilent(t *testing.T) {
	p := consolePool(t, false)
	got, restore := logged(t)
	defer restore()
	for _, mode := range []string{"log", "more"} {
		html, err := renderMode(context.Background(), p, "/", mode)
		if err != nil || html != "<p>done</p>" {
			t.Errorf("%s: %q %v", mode, html, err)
		}
	}
	if len(*got) != 0 {
		t.Errorf("logged %q", *got)
	}
}

// error and a failed assert fail the render in both modes, and the dev log keeps what the page
// wrote before it failed.
func TestConsoleErrorAndAssertFailTheRenderInBothModes(t *testing.T) {
	for _, dev := range []bool{false, true} {
		p := consolePool(t, dev)
		got, restore := logged(t)
		for mode, want := range map[string]string{"error": "broken 1", "assert": "Assertion failed: bad"} {
			if _, err := renderMode(context.Background(), p, "/", mode); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("dev=%v %s: error %v, want %q", dev, mode, err, want)
			}
		}
		if _, err := renderMode(context.Background(), p, "/", "throw"); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("dev=%v throw: error %v", dev, err)
		}
		restore()
		var before []string
		for _, line := range *got {
			if strings.HasPrefix(line, "console.log: before") || strings.HasPrefix(line, "console.log: then") {
				before = append(before, line)
			}
		}
		if dev && len(before) != 2 || !dev && len(*got) != 0 {
			t.Errorf("dev=%v logged %q", dev, *got)
		}
	}
}

// The rest of the console does not throw. In dev table, dir and group with a label print as log.
func TestConsoleHasTheCommonMethods(t *testing.T) {
	for _, dev := range []bool{false, true} {
		p := consolePool(t, dev)
		got, restore := logged(t)
		html, err := renderMode(context.Background(), p, "/", "more")
		restore()
		if err != nil || html != "<p>done</p>" {
			t.Errorf("dev=%v: %q %v", dev, html, err)
		}
		var want []string
		if dev {
			at := " path=/ at=app/page.tsx:12:5"
			want = []string{`console.log: [{"a":1}]` + at, `console.log: {"b":2}` + at, `console.log: section` + at}
		}
		if !slices.Equal(*got, want) {
			t.Errorf("dev=%v logged %q, want %q", dev, *got, want)
		}
	}
}

// A page that logs much, or logs a value that cannot be shown, costs the render nothing.
func TestDevConsoleKeepsItsLimits(t *testing.T) {
	p := consolePool(t, true)
	got, restore := logged(t)
	defer restore()
	if html, err := renderMode(context.Background(), p, "/", "long"); err != nil || html != "<p>done</p>" {
		t.Fatalf("%q %v", html, err)
	}
	if n := len(*got); n < 50 || n > 110 {
		t.Errorf("%d lines logged", n)
	}
	if last := (*got)[len(*got)-1]; !strings.Contains(last, "more console calls") {
		t.Errorf("last line %q", last)
	}
	for _, line := range *got {
		if len(line) > 5000 {
			t.Errorf("a line of %d bytes", len(line))
		}
	}
	*got = nil
	if html, err := renderMode(context.Background(), p, "/", "bad"); err != nil || html != "<p>done</p>" {
		t.Fatalf("%q %v", html, err)
	}
	if len(*got) != 2 {
		t.Errorf("logged %q", *got)
	}
}

// tifa dev renders a page a second time to find what it reads from the clock. That render logs
// nothing, so a call is not printed twice, and the list is empty for the next render.
func TestDevConsoleIsNotLoggedByTheSecondRender(t *testing.T) {
	p := consolePool(t, true)
	got, restore := logged(t)
	defer restore()
	ctx, _ := WithReads(context.Background())
	if _, err := renderMode(ctx, p, "/", "log"); err != nil {
		t.Fatal(err)
	}
	if _, err := renderMode(context.Background(), p, "/", "ok"); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 0 {
		t.Errorf("logged %q", *got)
	}
}
