package ssr

import (
	"os"
	"testing"
)

// What a page finds when it renders on the server: the host script of every SSR bundle, Intl and
// the engine itself. docs/app.md lists the same in "What the server render has", so a change here
// is a change to that section.

func hostPool(t *testing.T) *Pool {
	t.Helper()
	host, err := os.ReadFile("../../js/runtime/host.js")
	if err != nil {
		t.Fatal(err)
	}
	return newTestPool(t, string(host)+"\n"+exprBundle, testConfig(1))
}

// attempt wraps a JS expression so that a throw becomes its value, as "throws Name: message".
func attempt(expr string) string {
	return "(() => { try { return String(" + expr + ") } catch (e) { return 'throws ' + e.name + ': ' + e.message } })()"
}

func TestServerRenderGlobals(t *testing.T) {
	missing := []string{
		"setTimeout", "setInterval", "clearTimeout", "requestAnimationFrame", "requestIdleCallback",
		"performance", "window", "document", "navigator", "self", "location", "localStorage",
		"matchMedia", "ResizeObserver", "IntersectionObserver", "MessageChannel",
		"fetch", "Request", "XMLHttpRequest", "Blob", "FormData", "Event", "EventTarget",
		"AbortSignal", "process", "global", "Buffer", "require", "WebAssembly",
	}
	present := []string{
		"queueMicrotask", "URL", "URLSearchParams", "TextEncoder", "TextDecoder", "AbortController",
		"structuredClone", "crypto", "DOMException", "atob", "btoa", "Headers", "Response", "WeakRef", "FinalizationRegistry", "Proxy", "Intl", "console",
	}
	var exprs []string
	for _, name := range append(missing, present...) {
		exprs = append(exprs, "typeof globalThis."+name)
	}
	got := renderExprs(t, hostPool(t), exprs...)
	for i, name := range missing {
		if got[i] != "undefined" {
			t.Errorf("%s is %s on the server", name, got[i])
		}
	}
	for i, name := range present {
		if got[len(missing)+i] == "undefined" {
			t.Errorf("%s is missing on the server", name)
		}
	}
}

func TestServerRenderBehavior(t *testing.T) {
	cases := []struct{ expr, want string }{
		// console: log, info, debug and warn print nothing, error and a failed assert fail the render.
		{"console.log('x')", "undefined"},
		{"console.warn('x')", "undefined"},
		{"console.error('broken', 1)", "throws Error: broken 1"},
		{"console.assert(false, 'bad')", "throws Error: Assertion failed: bad"},
		{"console.assert(true, 'fine')", "undefined"},
		{"console.time('x')", "throws TypeError: not a function"},
		// TextDecoder knows UTF-8 only.
		{"new TextDecoder().encoding", "utf-8"},
		{"new TextDecoder('latin1')", "throws RangeError: Encoding not supported: latin1"},
		// localeCompare sorts by the locale's collation, as Chromium does.
		{"['张', '李', 'a', 'B'].sort((a, b) => a.localeCompare(b)).join()", "a,B,张,李"},
		// A maximumFractionDigits below the currency's digits lowers the minimum to match, and
		// fraction digits are not read next to significant digits, as in browsers. A minimum
		// above a given maximum still throws.
		{"new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY', maximumFractionDigits: 0}).format(5.4)", "¥5"},
		{"new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY', minimumFractionDigits: 0, maximumFractionDigits: 0}).format(5)", "¥5"},
		{"new Intl.NumberFormat('en-US', {maximumSignificantDigits: 3, maximumFractionDigits: 200}).format(1.2345)", "1.23"},
		{"new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY', minimumFractionDigits: 3}).format(5)", "¥5.000"},
		{"new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY', minimumFractionDigits: 3, maximumFractionDigits: 1}).format(5)", "throws RangeError: intl: an option is out of range: at least 3 decimals but at most 1"},
		// The clock, random numbers and the compiler are there. Pages must not let the first two
		// reach the HTML, see "SSR and cache".
		{"typeof Date.now()", "number"},
		{"typeof Math.random()", "number"},
		{"new Function('return 1')()", "1"},
	}
	var exprs []string
	for _, tc := range cases {
		exprs = append(exprs, attempt(tc.expr))
	}
	got := renderExprs(t, hostPool(t), exprs...)
	for i, tc := range cases {
		if got[i] != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, got[i], tc.want)
		}
	}
}
