package ssr

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// mapping is one segment of a test source map: a column of a line of the bundle, both from 0,
// and the place in a source it comes from.
type mapping struct {
	line, column        int
	source              string
	sourceLine, sourceC int
}

// sourceMapOf writes the source map that holds segments, which come in the order of the bundle.
func sourceMapOf(segments ...mapping) []byte {
	const digits = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	vlq := func(n int) string {
		v := n << 1
		if n < 0 {
			v = -n<<1 | 1
		}
		s := ""
		for {
			d := v & 31
			v >>= 5
			if v > 0 {
				d |= 32
			}
			s += string(digits[d])
			if v == 0 {
				return s
			}
		}
	}
	var sources []string
	index := map[string]int{}
	var lines []string
	src, srcLine, srcCol := 0, 0, 0
	for _, s := range segments {
		for len(lines) <= s.line {
			lines = append(lines, "")
		}
		i, ok := index[s.source]
		if !ok {
			i = len(sources)
			index[s.source] = i
			sources = append(sources, s.source)
		}
		prev := 0
		if text := lines[s.line]; text != "" {
			for _, m := range segments {
				if m.line == s.line && m.column < s.column {
					prev = m.column
				}
			}
			lines[s.line] += ","
		}
		lines[s.line] += vlq(s.column-prev) + vlq(i-src) + vlq(s.sourceLine-srcLine) + vlq(s.sourceC-srcCol)
		src, srcLine, srcCol = i, s.sourceLine, s.sourceC
	}
	data, err := json.Marshal(map[string]any{"version": 3, "sources": sources, "mappings": strings.Join(lines, ";")})
	if err != nil {
		panic(err)
	}
	return data
}

// requireBundle is a bundle whose second line is a package that calls require, and whose
// fourth line is page code. startRender fails by the URL: in its own call, in a promise as the
// runtime reports it, by another error, or after an emoji on the line.
var requireBundle = strings.Join([]string{
	"(function () {",
	"  function load(name) { return require(name) }",
	"  const fail = { '/sync': () => load('fs'), '/async': () => Promise.resolve().then(() => load('fs')), '/other': () => null.x, '/astral': () => astral() }",
	"  function page(url) { return fail[url]() }",
	"  function astral() { const s = '😀'; return require(s) }",
	"  globalThis.startRender = function (json) {",
	"    Promise.resolve(page(JSON.parse(json).url)).then((html) => { globalThis.renderResult = html }, (e) => {",
	"      globalThis.renderResult = JSON.stringify({ error: e.name + ': ' + e.message + '\\n' + e.stack }) })",
	"  }",
	"})()",
}, "\n")

// requireMap maps the require on line 2 to node_modules/pkg/index.js:3:10, the code of page to
// app/page.tsx:7:3, and the require after the emoji to node_modules/emoji/index.js:1:1.
func requireMap() []byte {
	lines := strings.Split(requireBundle, "\n")
	// The source map counts columns in UTF-16 units, the emoji is two.
	astral := len([]rune(lines[4][:strings.Index(lines[4], "require")])) + 1
	return sourceMapOf(
		mapping{1, 0, "node_modules/pkg/index.js", 0, 0},
		mapping{1, strings.Index(lines[1], "require"), "node_modules/pkg/index.js", 2, 9},
		mapping{3, 0, "app/page.tsx", 6, 2},
		mapping{4, 0, "node_modules/emoji/index.js", 0, 0},
		mapping{4, astral, "node_modules/emoji/index.js", 0, 0},
	)
}

const advice = "SSR runs in QuickJS, which has no require"

func TestRenderErrorsNameThePackageThatCallsRequire(t *testing.T) {
	p, err := NewPool(requireBundle, requireMap(), testConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for url, want := range map[string]string{
		"/sync":   "The package pkg calls require at node_modules/pkg/index.js:3:10",
		"/async":  "The package pkg calls require at node_modules/pkg/index.js:3:10",
		"/astral": "The package emoji calls require at node_modules/emoji/index.js:1:1",
	} {
		_, err := p.Render(context.Background(), Input{URL: url})
		if err == nil {
			t.Fatalf("%s: no error", url)
		}
		got := err.Error()
		if !strings.Contains(got, "ReferenceError: 'require' is not defined") || !strings.Contains(got, want) ||
			!strings.Contains(got, advice) || strings.Contains(got, "<eval>") {
			t.Errorf("%s: %s\nwant %s and why", url, got, want)
		}
		// A promise callback runs after the page returned.
		if url != "/async" && !strings.Contains(got, "(app/page.tsx:7:3)") {
			t.Errorf("%s: %s\nwant the page in the stack", url, got)
		}
	}
	_, err = p.Render(context.Background(), Input{URL: "/other"})
	if err == nil || !strings.Contains(err.Error(), "TypeError") || !strings.Contains(err.Error(), "(app/page.tsx:7:") ||
		strings.Contains(err.Error(), advice) {
		t.Errorf("another error: %v, want its stack in the sources and no word on require", err)
	}
}

func TestRenderErrorsWithoutASourceMapGivePlacesInTheBundle(t *testing.T) {
	p, err := NewPool(requireBundle, nil, testConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_, err = p.Render(context.Background(), Input{URL: "/sync"})
	if err == nil || !strings.Contains(err.Error(), "The code at ssr.js:2:32 calls require") || strings.Contains(err.Error(), "<eval>") {
		t.Errorf("err = %v, want the place in ssr.js", err)
	}
}

func TestPrewarmErrorsNameTheNodeModuleTheBundleLoads(t *testing.T) {
	// Rolldown passes the Node modules an IIFE imports as its arguments, and the tifa plugin
	// writes them as a require that names the packages that import them.
	bundle := "(function (fs) {\n  globalThis.startRender = function () {}\n})(require(`node:fs`,`esm-fs, other`))\n"
	_, err := NewPool(bundle, sourceMapOf(mapping{1, 0, "node_modules/pkg/index.js", 0, 0}), testConfig(1))
	if err == nil || !strings.Contains(err.Error(), `ssr: prewarm VM: ReferenceError: 'require' is not defined`) ||
		!strings.Contains(err.Error(), `The bundle loads the Node module "node:fs", imported by esm-fs, other, at ssr.js:3:4`) ||
		!strings.Contains(err.Error(), advice) {
		t.Errorf("err = %v, want the Node module and its packages", err)
	}

	bundle = "(function () {\n  function load(name) { return require(name) }\n  load('fs')\n})()\n"
	_, err = NewPool(bundle, sourceMapOf(mapping{1, 0, "node_modules/pkg/index.js", 2, 0}), testConfig(1))
	if err == nil || !strings.Contains(err.Error(), "The package pkg calls require at node_modules/pkg/index.js:3:1") {
		t.Errorf("err = %v, want the package", err)
	}
}

// allocated returns the bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// An error is explained after its render ended, outside the memory limit and the timeout of the
// VM, so what it costs must not grow with the bundle for each frame of its stack trace.
func TestExplainingAnErrorCostsLittleForALargeBundle(t *testing.T) {
	const size = 4 << 20
	code := "globalThis.x = '" + strings.Repeat("中", size/3) + "😀" + strings.Repeat("x", size/2) + "'"
	column := len([]rune(code)) - 1
	stack := strings.Repeat(fmt.Sprintf("    at render (<eval>:1:%d)\n", column), 135)
	err := &scriptError{text: "ReferenceError: 'require' is not defined\n" + stack}
	s := &sources{code: code, raw: sourceMapOf(mapping{0, 0, "node_modules/pkg/index.js", 0, 0})}
	var first, again error
	if n := allocated(func() { first = s.explain(err) }); n > size/2 {
		t.Errorf("the first error allocates %d bytes for a bundle of %d", n, size)
	}
	if n := allocated(func() { again = s.explain(err) }); n > 256<<10 {
		t.Errorf("another error allocates %d bytes", n)
	}
	if first.Error() != again.Error() || !strings.Contains(first.Error(), "The package pkg calls require at node_modules/pkg/index.js:1:1") {
		t.Errorf("err = %.300s", first)
	}
}

// Only the first frames of a deep stack are given in the sources, so the cost of an error has
// a limit.
func TestExplainingAnErrorGivesTheFirstFramesOfADeepStack(t *testing.T) {
	stack := strings.Repeat("    at load (<eval>:2:32)\n", maxFrames+500)
	err := (&sources{code: requireBundle, raw: requireMap()}).explain(&scriptError{text: "ReferenceError: 'require' is not defined\n" + stack})
	got := err.Error()
	if n := strings.Count(got, "(node_modules/pkg/index.js:3:10)"); n != maxFrames {
		t.Errorf("%d frames given, want %d", n, maxFrames)
	}
	if !strings.Contains(got, "\n    ... 500 more frames\n") || strings.Contains(got, "<eval>") {
		t.Errorf("err = %.2000s", got)
	}
}
