package ssr

import (
	"errors"
	"strings"
	"testing"

	"github.com/liustack/quickjs"
)

// URL and URLSearchParams of a server render come from the engine's package url, which Load
// registers before the bundle runs. The core-js in the host script finds them good and installs
// nothing of its own. docs/app.md says the same in "What the server render has".

func TestURLComesFromTheEngine(t *testing.T) {
	// Without the host script, the engine's URL is there.
	got := renderExprs(t, newTestPool(t, exprBundle, testConfig(1)),
		"typeof URL", "typeof URLSearchParams", "new URL('/a/b?c=1#d', 'http://localhost').href")
	want := []string{"function", "function", "http://localhost/a/b?c=1#d"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("without the host script: got %q, want %q", got, want)
	}
	// With it, the URL is still the engine's: core-js would add URL.createObjectURL and keep its
	// state in a property of each object.
	got = renderExprs(t, hostPool(t),
		"typeof URL.createObjectURL",
		"Object.getOwnPropertyNames(new URL('http://a/')).length",
		"Object.getOwnPropertyNames(new URLSearchParams('a=1')).length",
		"new URL('/store?b=1', 'http://localhost').searchParams.get('b')")
	want = []string{"undefined", "0", "0", "1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("with the host script: got %q, want %q", got, want)
	}
}

// A VM whose URL cannot be registered is an error of the pool, not a VM that falls back to the
// polyfill of the host script.
func TestURLRegisterFailureStopsThePool(t *testing.T) {
	broken := errors.New("url: broken")
	defer func(r func(*quickjs.VM, ...urlOption) error) { registerURL = r }(registerURL)
	registerURL = func(*quickjs.VM, ...urlOption) error { return broken }
	_, err := NewPool(exprBundle, nil, testConfig(1), false)
	if !errors.Is(err, broken) {
		t.Fatalf("NewPool: got %v, want %v", err, broken)
	}
}

// What a page can see of URL that differs from the core-js polyfill the server had before: no own
// property on a URL, hosts processed by UTS 46 with the Bidi and joiner checks, and other words in
// the message of a TypeError. In the browser a page has the browser's URL, which matches the
// first two.
func TestURLDifferencesFromCoreJS(t *testing.T) {
	cases := []struct{ expr, want string }{
		// core-js: "Symbol(state)_" and a number, the property it keeps its state in.
		{"Object.getOwnPropertyNames(new URL('http://a/b')).join()", ""},
		// The same with core-js: the mapping of UTS 46, then punycode.
		{"new URL('http://ＥＸＡＭＰＬＥ.com/').host", "example.com"},
		{"new URL('http://faß.de/').host", "xn--fa-hia.de"},
		// core-js: example.xn--com-qze. A label of Latin letters and an Arabic one breaks the Bidi
		// rule of UTS 46, which core-js does not check and browsers do.
		{attempt("new URL('http://example.comا/').host"), "throws TypeError: Failed to construct 'URL': Invalid URL"},
		// core-js: "Invalid scheme" for both.
		{attempt("new URL('nope')"), "throws TypeError: Failed to construct 'URL': Invalid URL"},
		{attempt("new URL('/a', 'nope')"), "throws TypeError: Failed to construct 'URL': Invalid URL"},
	}
	var exprs []string
	for _, c := range cases {
		exprs = append(exprs, c.expr)
	}
	got := renderExprs(t, hostPool(t), exprs...)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s = %q, want %q", c.expr, got[i], c.want)
		}
	}
}
