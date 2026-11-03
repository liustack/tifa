package ssr

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// readBundle reads the time and random numbers from a package, from the page and from the runtime
// of tifa, as the source map says, and once as it loads.
const readBundle = `function lib() { return Date.now() }
function page() { return '<p>' + new Date().getFullYear() + ' ' + Math.random() + ' ' + lib() + ' ' + new Date(0).getTime() + ' ' + __tifa_random_bytes__(4) + ' ' + (new Date(0) instanceof Date && new Date(0).constructor === Date) + '</p>' }
globalThis.startRender = () => { Date.now(); Math.random(); globalThis.renderResult = page() }
globalThis.loadedAt = Date.now()
`

func readMap() []byte {
	return sourceMapOf(
		mapping{0, 0, "node_modules/lib/index.js", 0, 0},
		mapping{1, 0, "app/page.tsx", 0, 0},
		mapping{2, 0, ".tifa/runtime/server.tsx", 0, 0},
		mapping{3, 0, "app/init.ts", 0, 0},
	)
}

// Under tifa dev a render says where the code of the app read the time or a random number. A read
// inside a package is named by the call of the app that led to it, and the reads of the runtime and
// those of the bundle as it loaded are left out. The page renders as without the check.
func TestDevRendersNameWhereTheAppReadTheTime(t *testing.T) {
	p, err := NewPool(readBundle, readMap(), testConfig(1), true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// A render nobody asks about forgets its reads.
	if _, err := p.Render(context.Background(), Input{URL: "/"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		ctx, reads := WithReads(context.Background())
		html, err := p.Render(ctx, Input{URL: "/"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(html, "<p>20") || !strings.HasSuffix(html, "== true</p>") || !strings.Contains(html, " 0 ") {
			t.Errorf("html %s", html)
		}
		got := reads.Places()
		want := []string{
			"new Date() at app/page.tsx:1:1",
			"Math.random() at app/page.tsx:1:1",
			"Date.now() at app/page.tsx:1:1",
			"crypto at app/page.tsx:1:1",
		}
		if !slices.Equal(got, want) {
			t.Errorf("places %q, want %q", got, want)
		}
	}
}

// A built app neither wraps nor writes down anything.
func TestBuiltRendersNameNoReads(t *testing.T) {
	p, err := NewPool(readBundle, readMap(), testConfig(1), false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, reads := WithReads(context.Background())
	if _, err := p.Render(ctx, Input{URL: "/"}); err != nil {
		t.Fatal(err)
	}
	if got := reads.Places(); len(got) != 0 {
		t.Errorf("places %q", got)
	}
}

// A subclass of Date keeps its prototype under the check of tifa dev, also when a page puts it in
// place of Date.
func TestDevKeepsSubclassesOfDate(t *testing.T) {
	const bundle = `globalThis.startRender = () => {
  class CustomDate extends Date { hi() { return 'hi' } }
  const saved = globalThis.Date
  globalThis.Date = CustomDate
  const d = new Date(0)
  globalThis.Date = saved
  const e = new CustomDate(0)
  globalThis.renderResult = '<p>' + (d instanceof CustomDate) + ' ' + (d instanceof Date) + ' ' + d.hi() + ' ' + (e instanceof CustomDate) + ' ' + e.getTime() + '</p>'
}`
	for _, dev := range []bool{false, true} {
		p, err := NewPool(bundle, nil, testConfig(1), dev)
		if err != nil {
			t.Fatal(err)
		}
		html, err := p.Render(context.Background(), Input{URL: "/"})
		p.Close()
		if err != nil || html != "<p>true true hi true 0</p>" {
			t.Errorf("dev %t: %q %v", dev, html, err)
		}
	}
}
