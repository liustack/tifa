package ssr

import (
	"context"
	"strings"
	"testing"
)

// The bundle is an ES module. Its top-level await finishes while each VM loads, before the first
// render, running the jobs it waits for, and every render of the VM sees what it made.
func TestTopLevelAwaitFinishesBeforeTheFirstRender(t *testing.T) {
	bundle := "const greeting = await new Promise((resolve) => Promise.resolve().then(() => resolve('hello')))\n" +
		"globalThis.loads = (globalThis.loads ?? 0) + 1\n" +
		"globalThis.startRender = function () { globalThis.renderResult = '<p>' + greeting + ' ' + globalThis.loads + '</p>' }\n"
	p := newTestPool(t, bundle, testConfig(2))
	for range 4 {
		html, err := p.Render(context.Background(), input("ok", ""))
		if err != nil {
			t.Fatal(err)
		}
		if html != "<p>hello 1</p>" {
			t.Fatalf("got %q, want the awaited value, loaded once in its VM", html)
		}
	}
}

// A top-level await that nothing settles, as one waiting for a timer would, fails the prewarm: a
// VM has no timers and does no I/O, so it would wait for good.
func TestTopLevelAwaitThatNeverSettlesFailsThePrewarm(t *testing.T) {
	bundle := "await new Promise(() => {})\nglobalThis.startRender = function () {}\n"
	_, err := NewPool(bundle, nil, testConfig(1), false)
	if err == nil || !strings.Contains(err.Error(), "ssr: prewarm VM:") || !strings.Contains(err.Error(), "top-level await is still pending") {
		t.Fatalf("err = %v, want the pending top-level await", err)
	}
}
