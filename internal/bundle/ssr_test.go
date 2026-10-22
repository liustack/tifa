package bundle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/quickjs"

	"github.com/liustack/tifa/internal/ssr"
	"github.com/liustack/tifa/js"
)

// buildScript builds entry of root like an SSR bundle with the tifa plugin and fails the test on
// an error.
func buildScript(t *testing.T, root, entry string) (string, []string) {
	t.Helper()
	code, modules, err := script(testdata(t), root, entry, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return code, modules
}

// buildPrinting builds entry of root like buildScript and returns the code and what the build
// printed, its warnings among it.
func buildPrinting(t *testing.T, root, entry string) (string, string) {
	t.Helper()
	var output strings.Builder
	code, _, err := script(testdata(t), root, entry, &output)
	if err != nil {
		t.Fatal(err)
	}
	return code, output.String()
}

func hasModule(modules []string, suffix string) bool {
	return slices.ContainsFunc(modules, func(m string) bool { return strings.HasSuffix(m, suffix) })
}

func TestSSRTrimmedReactRendersOnlyTheStringRenderer(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	code, modules := buildScript(t, root, "render.tsx")
	if hasModule(modules, "react-dom-server.edge.production.js") {
		t.Error("the stream renderer is bundled")
	}
	if !hasModule(modules, "react-dom-server-legacy.browser.production.js") {
		t.Errorf("the string renderer is missing: %v", modules)
	}
	html := evalResult(t, code)
	if !strings.Contains(html, "&lt;script&gt;订单 &amp; 🙂&lt;/script&gt;") {
		t.Errorf("text not escaped: %s", html)
	}
	if !strings.Contains(html, "<!--$-->") {
		t.Errorf("Suspense boundary marker missing: %s", html)
	}
}

// renderToString leaves a Suspense boundary whose content threw to the browser and tells no one
// of the error. The tifa plugin gives it the onError option of React's other renderers, so the
// server can report the error.
func TestSSRReportsTheErrorAComponentThrowsInsideSuspense(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"throws.tsx": "import './host'\n" +
		"import { renderToString } from 'react-dom/server.edge'\nimport { Suspense } from 'react'\n" +
		"const errors: unknown[] = []\n" +
		"function Broken(): never {\n  throw new Error('BROKEN')\n}\n" +
		"const html = renderToString(<Suspense fallback=\"loading\"><Broken /></Suspense>, { onError: (e: unknown) => errors.push(e) } as any)\n" +
		"globalThis.result = html + ' ' + errors.map((e) => (e as Error).message).join()\n",
	})
	code, _ := buildScript(t, root, "throws.tsx")
	if got := evalResult(t, code); !strings.HasPrefix(got, "<!--$!-->") || !strings.HasSuffix(got, " BROKEN") {
		t.Errorf("result = %q, want the boundary left to the browser and the error", got)
	}
}

// A render inside a render reports its errors to its own onError, and to no one without one, as
// React's other renderers do. The outer render gets only its own.
func TestSSRReportsTheErrorsOfEachRenderToItsOwnOnError(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"nested.tsx": "import './host'\n" +
		"import { renderToString, renderToStaticMarkup } from 'react-dom/server.edge'\nimport { Suspense } from 'react'\n" +
		"const outer: Error[] = [], inner: Error[] = []\n" +
		"function Inner(): never { throw new Error('INNER') }\n" +
		"function Outer(): never {\n" +
		"  renderToString(<Suspense fallback=\"a\"><Inner /></Suspense>)\n" +
		"  renderToStaticMarkup(<Suspense fallback=\"b\"><Inner /></Suspense>, { onError: (e: Error) => inner.push(e) } as any)\n" +
		"  throw new Error('OUTER')\n}\n" +
		"renderToString(<Suspense fallback=\"c\"><Outer /></Suspense>, { onError: (e: Error) => outer.push(e) } as any)\n" +
		"renderToString(<Suspense fallback=\"d\"><Inner /></Suspense>)\n" +
		"globalThis.result = [outer, inner].map((l) => l.map((e) => e.message).join()).join(' ')\n",
	})
	code, _ := buildScript(t, root, "nested.tsx")
	if got := evalResult(t, code); got != "OUTER INNER" {
		t.Errorf("result = %q, want OUTER INNER", got)
	}
}

// escapes.tsx renders, three times over, texts and attribute values that need escaping and some
// that do not: every character React escapes, text that is not ASCII and a lone surrogate, values
// that are not strings, among them an object whose text changes on every call, enough escaped text
// to fill the tifa plugin's cache of React's escapes past its limit, and a text larger than that
// limit.
const escapesPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"const texts = ['plain', '', '&', '<>', `\"'`, 'a & b < c > d \" e \\' f', '订单 & 🙂', '\\ud800 & lone', 'x'.repeat(5000) + '&']\n" +
	"for (let i = 0; i < 16; i++) texts.push(`${i} &`.padEnd(4096, '<'))\n" +
	"texts.push('a&'.repeat(70000))\n" +
	"let calls = 0\n" +
	"const changing = { toString: () => `call ${calls++} & <` }\n" +
	"const values = [0, -0, 1.5, 10n, true, false, null, changing, ...texts.slice(0, 9)]\n" +
	"function Page() {\n" +
	"  return <div>\n" +
	"    {texts.map((t, i) => <p key={i} className={t}>{t}</p>)}\n" +
	"    {values.map((v, i) => <i key={i} title={v as any} data-v={v as any}>{typeof v === 'object' ? String(v) : (v as any)}</i>)}\n" +
	"  </div>\n" +
	"}\n" +
	"globalThis.result = [0, 1, 2].map(() => renderToString(<Page />)).join('\\n')\n"

// escapeCacheChars is the size of the plugin's cache of escapes, in UTF-16 code units. The
// minified bundle keeps it as a number, so it shows whether the cache is in the bundle.
const escapeCacheChars = "262144"

// searchedWithExec finds the search of the cache of escapes for a character to escape, with exec,
// in a minified bundle.
var searchedWithExec = regexp.MustCompile(`if\(\w+\.exec\(\w+\)===null\)return \w+;var \w+=\w+\.get\(`)

// The tifa plugin caches the escapes of React's string renderer, and pages render the same with
// the cache as without it.
func TestSSRCachesTheEscapesOfReact(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"escapes.tsx": escapesPage})
	code, printed := buildPrinting(t, root, "escapes.tsx")
	if !strings.Contains(code, escapeCacheChars) {
		t.Error("the bundle has no cache of escapes")
	}
	if !searchedWithExec.MatchString(code) {
		t.Error("the cache of escapes does not search with exec")
	}
	if strings.Contains(printed, "escape") {
		t.Errorf("the build prints:\n%s", printed)
	}
	cached := evalResult(t, code)
	for _, want := range []string{"<p class=\"a &amp; b &lt; c &gt; d &quot; e &#x27; f\">", "call 0 &amp; &lt;", "call 5 &amp; &lt;", strings.Repeat("a&amp;", 3)} {
		if !strings.Contains(cached, want) {
			t.Errorf("the page has no %s", want)
		}
	}
	for name, change := range map[string][2]string{
		"function": {"function escapeTextForBrowser(text) {", "function escapeTextForBrowser(text) /* changed */ {"},
		"regexp":   {"matchHtmlRegExp = /[\"'&<>]/;", "matchHtmlRegExp = /[<>\"'&]/;"},
	} {
		changed := withReactDOM(t, change[0], change[1])
		writeTree(t, changed, map[string]string{"escapes.tsx": escapesPage})
		code, printed := buildPrinting(t, changed, "escapes.tsx")
		if strings.Contains(code, escapeCacheChars) {
			t.Errorf("%s: the bundle has a cache of escapes", name)
		}
		if !strings.Contains(printed, "react-dom 19.3.0: ") || !strings.Contains(printed, "the tifa plugin does not cache the escapes") {
			t.Errorf("%s: the build prints:\n%s", name, printed)
		}
		if got := evalResult(t, code); got != cached {
			t.Errorf("%s: the page renders otherwise without the cache", name)
		}
	}
}

// everyEscapePage renders, as text and as attribute values, every UTF-16 code unit between two
// ampersands, all of them in a row, and 3,000 strings of up to 40 code units drawn from the
// characters React escapes, letters, lone surrogates and characters outside Latin-1. The cache of
// escapes has seen none of them, so React escapes each one itself.
const everyEscapePage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"const units = Array.from({ length: 0x10000 }, (_, c) => String.fromCharCode(c))\n" +
	"const texts = [units.join('&'), units.join(''), units.join('<a>')]\n" +
	"const alphabet = ['a', 'b', ' ', '&', '<', '>', '\"', \"'\", '&amp;', '\\ud800', '\\udc00', 'é', '订', '🙂']\n" +
	"let seed = 1\n" +
	"const random = (n: number) => ((seed = (seed * 1103515245 + 12345) % 2147483648), seed % n)\n" +
	"for (let i = 0; i < 3000; i++) {\n" +
	"  let s = ''\n" +
	"  for (let n = random(41); n > 0; n--) s += alphabet[random(alphabet.length)]\n" +
	"  texts.push(s)\n" +
	"}\n" +
	"globalThis.result = renderToString(<div>{texts.map((t, i) => <p key={i} title={t}>{t}</p>)}</div>)\n"

// escapedAtOnce finds the escape of & with replaceAll in a minified bundle.
var escapedAtOnce = regexp.MustCompile("\\.replaceAll\\(`&`,`&amp;`\\)")

// React's string renderer escapes a string one character at a time. The tifa plugin escapes it
// with replaceAll, once for each character React escapes, and pages render the same as without
// that.
func TestSSREscapesEachCharacterAtOnce(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"escapes.tsx": everyEscapePage})
	code, printed := buildPrinting(t, root, "escapes.tsx")
	if !escapedAtOnce.MatchString(code) {
		t.Error("the bundle does not escape with replaceAll")
	}
	if strings.Contains(printed, "react-dom") {
		t.Errorf("the build prints:\n%s", printed)
	}
	escaped := evalResult(t, code)
	for _, want := range []string{"&amp;&lt;&amp;", "&amp;&gt;&amp;", "&amp;&quot;&amp;", "&amp;&#x27;&amp;", "&amp;amp;"} {
		if !strings.Contains(escaped, want) {
			t.Errorf("the page has no %s", want)
		}
	}
	changed := withReactDOM(t, "for (index = match.index; index < text.length; index++) {", "for (index = match.index; text.length > index; index++) {")
	writeTree(t, changed, map[string]string{"escapes.tsx": everyEscapePage})
	code, printed = buildPrinting(t, changed, "escapes.tsx")
	if escapedAtOnce.MatchString(code) {
		t.Error("the bundle escapes with replaceAll in a React DOM it does not know")
	}
	if !strings.Contains(printed, "react-dom 19.3.0: ") || !strings.Contains(printed, "the tifa plugin does not escape them with replaceAll") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if got := evalResult(t, code); got != escaped {
		t.Error("the page renders otherwise with React's loop over the characters")
	}
}

// shortEscapesPage renders 36,920 short texts that need escaping, all different, and drops the
// HTML. It keeps renderToString, as the runtime does, so React's module and the tifa plugin's cache
// of escapes in it stay in the VM. A text takes 2 code units and its escape 5: counted by their
// code units alone, all of them would fit in the cache, in about 5 MB.
const shortEscapesPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"const texts = Array.from({ length: 36920 }, (_, i) => '<' + String.fromCharCode(0x100 + i))\n" +
	"renderToString(<div>{texts.map((t, i) => <i key={i}>{t}</i>)}</div>)\n" +
	"globalThis.render = renderToString\n" +
	"globalThis.result = 'done'\n"

// Many short strings take no more memory in the cache of escapes than a few long ones.
func TestSSRCachesShortEscapesInLittleMemory(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"escapes.tsx": shortEscapesPage})
	cached, _ := buildPrinting(t, root, "escapes.tsx")
	plain := withReactDOM(t, "function escapeTextForBrowser(text) {", "function escapeTextForBrowser(text) /* changed */ {")
	writeTree(t, plain, map[string]string{"escapes.tsx": shortEscapesPage})
	uncached, _ := buildPrinting(t, plain, "escapes.tsx")
	memory := func(code string) int64 {
		vm, err := quickjs.NewVM()
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()
		if _, err := vm.Eval(code, quickjs.EvalGlobal); err != nil {
			t.Fatalf("evaluate bundle: %v", err)
		}
		return ssr.VMMemory(vm, true).Fmalloc_size
	}
	if grown := memory(cached) - memory(uncached); grown > 1<<20 {
		t.Errorf("the cache of escapes takes %d KiB, want at most 1 MiB", grown>>10)
	}
}

// segmentsPage renders, three times over, Suspense boundaries side by side, nested in each other,
// at the start and the end of their parents and one whose content never arrives, with text,
// attributes, an empty text and hoistable tags around them. Each boundary adds a child segment to
// the segment around it, so React writes the chunks of a segment between its children.
const segmentsPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"import { Suspense, lazy } from 'react'\n" +
	"const Never = lazy(() => new Promise<never>(() => {}))\n" +
	"function Item({ n }: { n: number }) {\n" +
	"  return <li data-n={n} className={n % 2 ? 'odd' : undefined}>item {n}{''}<b>{n * 1.5}</b></li>\n" +
	"}\n" +
	"function Page() {\n" +
	"  return <main>\n" +
	"    <Suspense fallback=\"a\"><p>first</p></Suspense>\n" +
	"    <title>{'segments & <chunks>'}</title>\n" +
	"    <meta name=\"x\" content=\"y\" />\n" +
	"    <link rel=\"stylesheet\" href=\"/s.css\" precedence=\"default\" />\n" +
	"    <h1 title=\"t\">head {'&'} text</h1>\n" +
	"    <Suspense fallback=\"b\">\n" +
	"      <ul>{[1, 2, 3].map((n) => <Item key={n} n={n} />)}</ul>\n" +
	"      <Suspense fallback=\"c\"><Suspense fallback=\"d\">deep</Suspense>between<Item n={4} /></Suspense>\n" +
	"      tail\n" +
	"    </Suspense>\n" +
	"    <Suspense fallback=\"e\"><Never /></Suspense>\n" +
	"    {''}\n" +
	"    <Suspense fallback=\"f\">last</Suspense>\n" +
	"  </main>\n" +
	"}\n" +
	"globalThis.result = [0, 1, 2].map(() => renderToString(<Page />)).join('\\n')\n"

// React's string renderer hands the chunks of a segment one by one to the function that appends
// them to the HTML. The tifa plugin joins the chunks between two children of a segment and hands
// them over at once, and pages render the same as without that.
func TestSSRJoinsTheChunksOfEachSegment(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"segments.tsx": segmentsPage})
	code, printed := buildPrinting(t, root, "segments.tsx")
	if !joinedChunks.MatchString(code) {
		t.Error("the bundle does not join the chunks of a segment")
	}
	if strings.Contains(printed, "react-dom") {
		t.Errorf("the build prints:\n%s", printed)
	}
	joined := evalResult(t, code)
	for _, want := range []string{"<!--$--><p>first</p><!--/$-->", "<!--$--><!--$-->deep<!--/$-->between", "<!--$!--><template></template>e<!--/$-->", "<!--$-->last<!--/$-->"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the page has no %s:\n%s", want, joined)
		}
	}
	changed := withReactDOM(t, "for (r = segment[childIdx]; chunkIdx < r.index; chunkIdx++)", "for (r = segment[childIdx]; r.index > chunkIdx; chunkIdx++)")
	writeTree(t, changed, map[string]string{"segments.tsx": segmentsPage})
	code, printed = buildPrinting(t, changed, "segments.tsx")
	if joinedChunks.MatchString(code) {
		t.Error("the bundle joins the chunks of a segment of a React DOM it does not know")
	}
	if !strings.Contains(printed, "react-dom 19.3.0: ") || !strings.Contains(printed, "the tifa plugin does not join the chunks") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if got := evalResult(t, code); got != joined {
		t.Errorf("the page renders otherwise without the joined chunks:\n%s\nwant:\n%s", got, joined)
	}
}

// joinedChunks finds the chunks of a segment joined by the tifa plugin in a minified bundle.
var joinedChunks = regexp.MustCompile("\\.slice\\([^()]*\\)\\.join\\(``\\)")

// stacksPage renders, three times over, elements of each kind React puts on its component
// stacks: function and class components, memo, forwardRef, context, fragments, keyed lists and
// Suspense boundaries, one of which throws. It also renders a component that throws outside a
// Suspense boundary, and records how many arguments onError gets and the errors.
const stacksPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"import { Component, Fragment, Suspense, createContext, forwardRef, memo, useContext } from 'react'\n" +
	"const Theme = createContext('light')\n" +
	"function Leaf({ n }: { n: number }) {\n  return <i data-n={n}>{useContext(Theme)} {n}</i>\n}\n" +
	"const Memo = memo(Leaf)\n" +
	"const Ref = forwardRef<HTMLSpanElement, { n: number }>(({ n }, ref) => <span ref={ref}><Memo n={n} /></span>)\n" +
	"class Box extends Component<{ children: any }> {\n  render() {\n    return <section>{this.props.children}</section>\n  }\n}\n" +
	"function Broken(): never {\n  throw new Error('BROKEN')\n}\n" +
	"function Page() {\n" +
	"  return <Theme.Provider value=\"dark\">\n" +
	"    <Box>{[1, 2, 3].map((n) => <Fragment key={n}><Ref n={n} /><Leaf n={n} /></Fragment>)}</Box>\n" +
	"    <Suspense fallback=\"a\"><Box><Memo n={4} /></Box></Suspense>\n" +
	"    <Suspense fallback=\"b\"><Leaf n={5} /><Broken /></Suspense>\n" +
	"    <Theme.Consumer>{(theme) => <b>{theme}</b>}</Theme.Consumer>\n" +
	"  </Theme.Provider>\n" +
	"}\n" +
	"const lines: string[] = []\n" +
	"for (let i = 0; i < 3; i++) {\n" +
	"  const calls: string[] = []\n" +
	"  lines.push(renderToString(<Page />, { onError: function () { calls.push(arguments.length + ' ' + arguments[0].message) } } as any))\n" +
	"  lines.push(calls.join())\n" +
	"}\n" +
	"try {\n  renderToString(<Box><Broken /></Box>)\n} catch (err) {\n  lines.push('throws ' + (err as Error).message)\n}\n" +
	"globalThis.result = lines.join('\\n')\n"

// componentStack finds where React puts an element on a component stack in a minified bundle.
var componentStack = regexp.MustCompile(`componentStack=\{parent:[\w$]+\.componentStack,type:`)

// React's string renderer puts each element it renders on a component stack, which it only gives
// the error handler of renderToString. The tifa plugin drops what that handler gets besides the
// error, and the component stacks with it, and pages render the same as with them.
func TestSSRBuildsNoComponentStacks(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"stacks.tsx": stacksPage})
	code, printed := buildPrinting(t, root, "stacks.tsx")
	if componentStack.MatchString(code) {
		t.Error("the bundle builds component stacks")
	}
	if strings.Contains(printed, "react-dom") {
		t.Errorf("the build prints:\n%s", printed)
	}
	unstacked := evalResult(t, code)
	for _, want := range []string{"<i data-n=\"3\">dark<!-- --> <!-- -->3</i>", "<!--$!--><template></template>b<!--/$-->", "\n1 BROKEN\n", "throws BROKEN"} {
		if !strings.Contains(unstacked, want) {
			t.Errorf("the page has no %q:\n%s", want, unstacked)
		}
	}
	for name, change := range map[string][2]string{
		"stack":   {"task.componentStack = { parent: task.componentStack, type: node.type };", "task.componentStack = { type: node.type, parent: task.componentStack };"},
		"onError": {"function onError() {}", "function onError() { }"},
		// Another reader of the stacks: a call of getThrownInfo, and a read of a stack alone.
		"thrown": {"function onError() {}", "function tifaThrown(task) {\n  return getThrownInfo(task.componentStack);\n}\nfunction onError() {}"},
		"read":   {"function onError() {}", "function tifaStack(task) {\n  return task.componentStack;\n}\nfunction onError() {}"},
	} {
		changed := withReactDOM(t, change[0], change[1])
		writeTree(t, changed, map[string]string{"stacks.tsx": stacksPage})
		code, printed := buildPrinting(t, changed, "stacks.tsx")
		if name == "stack" && !regexp.MustCompile(`componentStack=\{type:`).MatchString(code) || name != "stack" && !componentStack.MatchString(code) {
			t.Errorf("%s: the bundle builds no component stacks", name)
		}
		if name != "onError" && !strings.Contains(printed, "the tifa plugin does not keep React from building component stacks") {
			t.Errorf("%s: the build prints:\n%s", name, printed)
		}
		if name == "onError" {
			continue // React's own onError: the errors go nowhere
		}
		if got := evalResult(t, code); got != unstacked {
			t.Errorf("%s: the page renders otherwise with component stacks:\n%s\nwant:\n%s", name, got, unstacked)
		}
	}
}

// namespacesPage imports React, React DOM and abort-controller, which are CommonJS packages, as
// namespaces, from a TSX module and from an .mjs one, which rolldown imports in Node mode. It
// renders through them and gives, for an export of each, whether its namespace has a plain value
// for it or a getter. An ES module that a CommonJS module requires declares the names of rolldown's
// helpers, so rolldown renames its own, as it does in larger apps.
const namespacesPage = "import './host'\n" +
	"import * as React from 'react'\n" +
	"import ReactDefault from 'react'\n" +
	"import * as ReactDOM from 'react-dom'\n" +
	"import * as AbortControllers from 'abort-controller'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"import { kind, nodeMode } from './node-mode.mjs'\n" +
	"import helpers from './helpers.cjs'\n" +
	"function Page() {\n  const [n] = React.useState(3)\n  return React.createElement('b', null, n, ReactDefault.Children.count([1, 2]))\n}\n" +
	"globalThis.result = [renderToString(React.createElement(Page)), kind(React, 'useState'), kind(ReactDOM, 'flushSync'),\n" +
	"  kind(AbortControllers, 'AbortController'), nodeMode(), ReactDefault === (React as any).default, helpers.names].join(' ')\n"

const nodeModeModule = "import * as R from 'react'\n" +
	"export const kind = (ns, name) => ('value' in Object.getOwnPropertyDescriptor(ns, name) ? 'value' : 'getter')\n" +
	"export const nodeMode = () => kind(R, 'useId') + ' ' + typeof R.default.useId\n"

// The modules that import React or React DOM as a namespace get plain values in place of getters,
// in Node mode too, and the other packages keep their getters.
func TestSSRGivesReactNamespacesPlainValues(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{
		"namespaces.tsx": namespacesPage,
		"node-mode.mjs":  nodeModeModule,
		"helpers.cjs":    "module.exports = require('./helpers.mjs')\n",
		"helpers.mjs":    "var __toESM = 't', __defProp = 'd', __copyProps = 'c', __commonJSMin = 'm'\nexport const names = __toESM + __defProp + __copyProps + __commonJSMin\n",
	})
	code, printed := buildPrinting(t, root, "namespaces.tsx")
	if printed != "" {
		t.Errorf("the build prints:\n%s", printed)
	}
	if got, want := evalResult(t, code), "<b>3<!-- -->2</b> value value getter value function true tdcm"; got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
}

// keyPathsPage renders, three times over, components of each kind with and without keys, nested
// in each other, useId, which React derives from the place of a component, and forms with
// useActionState and function actions, with and without a permalink. None of the actions is a
// server action.
const keyPathsPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"import { Fragment, Suspense, forwardRef, memo, useActionState, useId } from 'react'\n" +
	"function Field({ label }: { label: string }) {\n  const id = useId()\n  return <label htmlFor={id}>{label}<input id={id} /></label>\n}\n" +
	"const Memo = memo(Field)\n" +
	"const Ref = forwardRef<HTMLDivElement, { n: number }>(({ n }, ref) => <div ref={ref}><Memo label={'m' + n} /></div>)\n" +
	"function Counter({ permalink }: { permalink?: string }) {\n" +
	"  const [count, increment, pending] = useActionState((n: number) => n + 1, 0, permalink)\n" +
	"  return <form action={increment}><output>{count} {String(pending)}</output><button formAction={increment}>+</button></form>\n" +
	"}\n" +
	"function Page() {\n" +
	"  return <main>\n" +
	"    {['a', 'b'].map((k) => <Fragment key={k}><Field label={k} /><Ref n={1} /></Fragment>)}\n" +
	"    <Field label=\"c\" />\n" +
	"    <Suspense fallback=\"x\"><Counter /><Counter permalink=\"/count\" /></Suspense>\n" +
	"    {[1, 2].map((n) => <Ref key={n} n={n} />)}\n" +
	"  </main>\n" +
	"}\n" +
	"globalThis.result = [0, 1, 2].map(() => renderToString(<Page />)).join('\\n')\n"

// keyPath finds the key path React makes for an element in a minified bundle, with the variable
// that holds the name of its component.
var keyPath = regexp.MustCompile(`\[[\w$]+\.keyPath,([\w$]+),[\w$]+\]`)

// namesInKeyPaths reports whether a minified bundle puts the names of components into their key
// paths: whether the last value given to the variable of the name before the key path is another
// than null.
func namesInKeyPaths(t *testing.T, code string) bool {
	t.Helper()
	m := keyPath.FindStringSubmatchIndex(code)
	if m == nil {
		t.Fatal("the bundle makes no key paths")
	}
	name := code[m[2]:m[3]]
	values := regexp.MustCompile(`[,;{(]`+regexp.QuoteMeta(name)+`=([^,;]+)`).FindAllStringSubmatch(code[max(0, m[0]-500):m[0]], -1)
	if values == nil {
		t.Fatalf("the bundle gives %s no value before %s", name, code[m[0]:m[1]])
	}
	return values[len(values)-1][1] != "null"
}

// React's string renderer puts the name of each component it renders into the path of keys of the
// component, which only server actions read. The tifa plugin leaves the names out, and pages
// render the same as with them.
func TestSSRLeavesComponentNamesOutOfKeyPaths(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"keys.tsx": keyPathsPage})
	code, printed := buildPrinting(t, root, "keys.tsx")
	if namesInKeyPaths(t, code) {
		t.Error("the bundle puts the names of components into their key paths")
	}
	if printed != "" {
		t.Errorf("the build prints:\n%s", printed)
	}
	unnamed := evalResult(t, code)
	for _, want := range []string{"<label for=\"_R_", "<output>0<!-- --> <!-- -->false</output>", "javascript:throw new Error("} {
		if !strings.Contains(unnamed, want) {
			t.Errorf("the page has no %q:\n%s", want, unnamed)
		}
	}
	for name, change := range map[string][2]string{
		"names":     {"name = getComponentNameFromType(type),\n", "name = (getComponentNameFromType(type)),\n"},
		"formState": {"    void 0,\n    void 0\n  );\n  children.flushScheduled", "    void 0,\n    globalThis.formState\n  );\n  children.flushScheduled"},
		"request":   {"    onFatalError,\n    formState\n  );", "    onFatalError,\n    globalThis.formState\n  );"},
		// Another reader of the names: a key from a key path, and the name of a key path.
		"key":  {"function onError() {}", "function tifaKey(keyPath) {\n  return JSON.stringify([keyPath, null, 0]);\n}\nfunction onError() {}"},
		"name": {"function onError() {}", "function tifaName(keyPath) {\n  return keyPath[1];\n}\nfunction onError() {}"},
	} {
		changed := withReactDOM(t, change[0], change[1])
		writeTree(t, changed, map[string]string{"keys.tsx": keyPathsPage})
		code, printed := buildPrinting(t, changed, "keys.tsx")
		if !namesInKeyPaths(t, code) {
			t.Errorf("%s: the bundle leaves the names of components out of their key paths", name)
		}
		if !strings.Contains(printed, "react-dom 19.3.0: ") || !strings.Contains(printed, "the tifa plugin does not leave the names of components out") {
			t.Errorf("%s: the build prints:\n%s", name, printed)
		}
		if got := evalResult(t, code); got != unnamed {
			t.Errorf("%s: the page renders otherwise with the names:\n%s\nwant:\n%s", name, got, unnamed)
		}
	}
}

// A server action has a $$FORM_ACTION, and React writes a key for its form state from the key path
// of the component, names included. tifa has no server actions, and the build warns of each module
// with $$FORM_ACTION in its code.
func TestSSRWarnsOfServerActions(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{
		"actions.tsx": "import './host'\nimport { renderToString } from 'react-dom/server.edge'\nimport { action } from './action'\n" +
			"globalThis.result = renderToString(<form action={action}></form>)\n",
		"action.ts": "export function action() {}\n(action as any).$$FORM_ACTION = () => ({ name: 'a', action: '/a' })\n",
	})
	_, printed := buildPrinting(t, root, "actions.tsx")
	if !strings.Contains(printed, "action.ts: has $$FORM_ACTION in its code") {
		t.Errorf("the build prints:\n%s", printed)
	}
}

// The tifa plugin leaves the names of components out of their key paths because the server
// renders with renderToString, which takes no form state. The runtime imports no other renderer.
func TestRuntimeRendersOnlyWithRenderToString(t *testing.T) {
	server, err := js.Runtime.ReadFile("runtime/server.tsx")
	if err != nil {
		t.Fatal(err)
	}
	imports := regexp.MustCompile(`import\s*(\{[^}]*\}|[\w$*][^'"]*)\s*from\s*['"](react-dom[^'"]*)['"]`).FindAllStringSubmatch(string(server), -1)
	if len(imports) != 1 || imports[0][2] != "react-dom/server.edge" || strings.Join(strings.Fields(imports[0][1]), " ") != "{ renderToString as renderString }" {
		t.Errorf("js/runtime/server.tsx imports %q from React DOM, want only renderToString", imports)
	}
}

// switchesPage renders each name of NAMES, which the test replaces with a JSON list, and names
// that are no case of React's switches, as the tag of an element alone, with props and children,
// and inside each element that gives its children another format context, and as a prop of a few
// elements with values of each type. Each render goes on a line of its own, or the error it
// throws.
const switchesPage = "import './host'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"import { createElement as h } from 'react'\n" +
	"const names: string[] = [...NAMES, 'id', 'type', 'name', 'aria-label', 'data-x', 'my-element', 'fooBar', 'onclick',\n" +
	"  'xlinkHref', 'xml:lang', 'font-face', 'annotation-xml', 'missing-glyph', 'unknown', '', 'constructor', '__proto__', 'toString']\n" +
	"const parents = ['svg', 'math', 'foreignObject', 'select', 'optgroup', 'table', 'thead', 'tbody', 'tfoot', 'tr',\n" +
	"  'colgroup', 'noscript', 'head', 'html', 'body', 'p', 'picture', 'template']\n" +
	"const hosts = ['div', 'input', 'select', 'option', 'img', 'link', 'meta', 'script', 'style', 'title', 'textarea', 'svg', 'a', 'button', 'form', 'my-element']\n" +
	"const values: unknown[] = ['text', '', 'a & \"b\" <c>', 0, -1.5, NaN, true, false, null, undefined,\n" +
	"  () => {}, Symbol('s'), {}, ['a', 'b'], { __html: '<i>x</i>' }, 10n, { color: 'red', fontSize: 12 }]\n" +
	"const lines: string[] = []\n" +
	"const render = (element: any) => {\n" +
	"  try {\n" +
	"    lines.push(renderToString(element))\n" +
	"  } catch (err) {\n" +
	"    lines.push('throws ' + (err as Error).message)\n" +
	"  }\n" +
	"}\n" +
	"for (const name of names) {\n" +
	"  render(h(name, null))\n" +
	"  const element = h(name, { className: 'c', id: 'i', value: 'v', href: '/h', rel: 'stylesheet', precedence: 'p', src: '/s.js', async: true }, 'text')\n" +
	"  render(element)\n" +
	"  for (const parent of parents) render(h(parent, null, element, h(name, null)))\n" +
	"  for (const host of hosts) for (const value of values) render(h(host, { [name]: value }))\n" +
	"}\n" +
	"globalThis.result = lines.join('\\n')\n"

// The tifa plugin puts a Set check in front of the long switches on strings in React's string
// renderer, and pages render the same as without them.
func TestSSRGuardsTheSwitchesOfReact(t *testing.T) {
	renderer := filepath.Join(testModules(t), "react-dom", "cjs", "react-dom-server-legacy.browser.production.js")
	switches := guardedOnNode(t, []string{renderer}, true)[0].Switches
	if len(switches) == 0 {
		t.Fatal("guardSwitches changes no switch of React DOM")
	}
	var names []string
	for _, s := range switches {
		names = append(names, s.Cases...)
	}
	list, err := json.Marshal(names)
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(switchesPage, "NAMES", string(list), 1)
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"switches.tsx": page})
	code, printed := buildPrinting(t, root, "switches.tsx")
	if got := len(guardedSwitch.FindAllString(code, -1)); got != len(switches) {
		t.Errorf("the bundle has %d switches with a Set check, want %d", got, len(switches))
	}
	if strings.Contains(printed, "react-dom") {
		t.Errorf("the build prints:\n%s", printed)
	}
	guarded := evalResult(t, code)
	for _, want := range []string{"<div id=\"text\"></div>", "<input type=\"\"/>", "<svg><foreignObject class=\"c\"", "throws "} {
		if !strings.Contains(guarded, want) {
			t.Errorf("the page has no %s", want)
		}
	}
	// (0, x) compares the same value as x, but is no identifier, so the plugin changes no switch.
	plain := withChangedReactDOM(t, func(source string) string {
		return regexp.MustCompile(`switch \(([\w$]+)\) \{`).ReplaceAllString(source, "switch ((0, $1)) {")
	})
	writeTree(t, plain, map[string]string{"switches.tsx": page})
	code, printed = buildPrinting(t, plain, "switches.tsx")
	if guardedSwitch.MatchString(code) {
		t.Error("the bundle of a React DOM without switches on names has a switch with a Set check")
	}
	if !strings.Contains(printed, "react-dom 19.3.0: ") || !strings.Contains(printed, "has no switch the tifa plugin can put a Set check in front of") {
		t.Errorf("the build prints:\n%s", printed)
	}
	got := strings.Split(evalResult(t, code), "\n")
	want := strings.Split(guarded, "\n")
	if len(got) != len(want) {
		t.Fatalf("the page has %d renders without the Set checks, %d with them", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("render %d is otherwise without the Set checks:\n%s\nwant:\n%s", i, got[i], want[i])
			break
		}
	}
}

// The tifa plugin puts a Set check in front of a switch of React DOM after the code its other
// changes make shorter or longer, at the end of the renderer too.
func TestSSRGuardsTheSwitchesAfterTheOtherChanges(t *testing.T) {
	renderer := filepath.Join(testModules(t), "react-dom", "cjs", "react-dom-server-legacy.browser.production.js")
	switches := guardedOnNode(t, []string{renderer}, true)[0].Switches
	cases := ""
	for _, c := range "abcdefgh" {
		cases += "    case \"" + string(c) + "\":\n"
	}
	last := "function tifaSwitch(name) {\n  switch (name) {\n" + cases + "      return 1;\n    default:\n      return 0;\n  }\n}\n" +
		"globalThis.tifaSwitched = tifaSwitch(\"c\") + tifaSwitch(\"z\");\nfunction onError() {}"
	changed := withReactDOM(t, "function onError() {}", last)
	writeTree(t, changed, map[string]string{"keys.tsx": keyPathsPage + "globalThis.result += '\\n' + (globalThis as any).tifaSwitched\n"})
	code, printed := buildPrinting(t, changed, "keys.tsx")
	if got := len(guardedSwitch.FindAllString(code, -1)); got != len(switches)+1 {
		t.Errorf("the bundle has %d switches with a Set check, want %d", got, len(switches)+1)
	}
	if printed != "" {
		t.Errorf("the build prints:\n%s", printed)
	}
	if got := evalResult(t, code); !strings.HasSuffix(got, "\n1") {
		t.Errorf("the switch gives %q, want 1", got[strings.LastIndex(got, "\n")+1:])
	}
}

// guardedSwitch finds a switch with a Set check in a minified bundle.
var guardedSwitch = regexp.MustCompile(`if\(\w+\.has\(([\w$]+)\)\)switch\(([\w$]+)\)`)

// stringsPage serializes with seroval each UTF-16 code unit alone, twice and between two letters,
// many of them at the end of 63 code units, surrogate pairs, lone surrogates and every character seroval
// escapes, as values and as keys. Each serialization goes on a line of its own, with each
// surrogate written as \u and its code, since a lone one does not come out of QuickJS as it is.
const stringsPage = "import './host'\n" +
	"import { serialize } from 'seroval'\n" +
	"const strings: string[] = []\n" +
	"for (let c = 0; c < 0x10000; c++) {\n" +
	"  const s = String.fromCharCode(c)\n" +
	"  strings.push(s, s + s, 'a' + s + 'b')\n" +
	"  if (c < 0x800 || c % 64 === 0) strings.push(s.padStart(63, 'x'))\n" +
	"}\n" +
	"strings.push('🙂', 'a🙂', '🙂🙂', '\\ud83d', '\\ude42\\ud83d', 'x\\udc00y', '\"\\\\\\n\\r\\b\\t\\f<\\u2028\\u2029\\x00\\x0b\\x1f</script>')\n" +
	"for (let n = 0; n < 70; n++) strings.push('ab<\"'.repeat(n).slice(0, n), 'é'.repeat(n))\n" +
	"const lines: string[] = []\n" +
	"for (let i = 0; i < strings.length; i += 4096) {\n" +
	"  const chunk = strings.slice(i, i + 4096)\n" +
	"  lines.push(serialize(chunk), serialize(Object.fromEntries(chunk.map((s, k) => [s, k]))))\n" +
	"}\n" +
	"globalThis.result = lines.join('\\n').replace(/[\\ud800-\\udfff]/g, (c) => '\\\\u' + c.charCodeAt(0).toString(16))\n"

// seroval escapes a string shorter than 64 code units one character at a time, and the tifa
// plugin makes it use JSON.stringify from 2 on, which gives the same text.
func TestSSRSerializesShortStringsWithJSON(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"strings.tsx": stringsPage})
	code, printed := buildPrinting(t, root, "strings.tsx")
	if !serializesShortStrings(code) {
		t.Error("the bundle does not serialize strings of 2 code units with JSON.stringify")
	}
	if strings.Contains(printed, "seroval") {
		t.Errorf("the build prints:\n%s", printed)
	}
	short := evalResult(t, code)
	for _, want := range []string{`"a\x3Cb"`, `"\"\""`, `"\u2028\u2028"`, `"\ud83d"`} {
		if !strings.Contains(short, want) {
			t.Errorf("the serializations have no %s", want)
		}
	}
	changed := withChangedPackage(t, "seroval", filepath.Join("dist", "index.js"), func(source string) string {
		const length = "const MIN_JSON_STRINGIFY_LENGTH = 64;"
		if strings.Count(source, length) != 1 {
			t.Fatalf("seroval does not have %q once", length)
		}
		return strings.Replace(source, length, "const MIN_JSON_STRINGIFY_LENGTH = 32 * 2;", 1)
	})
	writeTree(t, changed, map[string]string{"strings.tsx": stringsPage})
	code, printed = buildPrinting(t, changed, "strings.tsx")
	if serializesShortStrings(code) {
		t.Error("the bundle of a seroval the plugin does not know serializes strings of 2 code units with JSON.stringify")
	}
	if !strings.Contains(printed, "seroval 1.6.9: ") || !strings.Contains(printed, "escapes strings shorter than 64") {
		t.Errorf("the build prints:\n%s", printed)
	}
	got := strings.Split(evalResult(t, code), "\n")
	want := strings.Split(short, "\n")
	if len(got) != len(want) {
		t.Fatalf("%d serializations without the change, %d with it", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("serialization %d is otherwise without the change", i)
		}
	}
}

// When seroval's main file is no longer where the tifa plugin looks for it, the build warns.
func TestSSRWarnsWhenSerovalMovedItsMainFile(t *testing.T) {
	moved := withChangedPackage(t, "seroval", "package.json", func(source string) string {
		for _, field := range []string{`"import": "./dist/index.js"`, `"default": "./dist/index.js"`} {
			if strings.Count(source, field) != 1 {
				t.Fatalf("the package.json of seroval does not have %s once", field)
			}
			source = strings.Replace(source, field, strings.Replace(field, "dist/", "dist/dev/", 1), 1)
		}
		return source
	})
	writeTree(t, moved, map[string]string{"short.ts": "import { serialize } from 'seroval'\nglobalThis.result = serialize(['ab', 'a<b'])\n"})
	code, printed := buildPrinting(t, moved, "short.ts")
	if serializesShortStrings(code) {
		t.Error("the bundle serializes strings of 2 code units with JSON.stringify")
	}
	if !strings.Contains(printed, "seroval 1.6.9 at ") || !strings.Contains(printed, "escapes strings shorter than 64") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if got := evalResult(t, code); got != `["ab","a\x3Cb"]` {
		t.Errorf("result = %s", got)
	}
}

// arraysPage serializes values with seroval and a plugin that writes the arrays and objects it
// marks as JSON text, as the tifa.data adapter does, in seroval's three ways: at once, async and
// as a stream, which TanStack Router takes. Each serialization goes on a line with the value its
// code gives back, written with JSON.stringify, which tells an array from an object.
const arraysPage = "import './host'\n" +
	"import { createPlugin, crossSerializeStream, serialize, serializeAsync } from 'seroval'\n" +
	"const marked = new WeakSet<object>()\n" +
	"const mark = <T extends object>(value: T) => (marked.add(value), value)\n" +
	"const asJSON = createPlugin<object, { text: any }>({\n" +
	"  tag: 'test.json',\n" +
	"  test: (value) => typeof value === 'object' && value !== null && marked.has(value),\n" +
	"  parse: {\n" +
	"    sync: (value, ctx) => ({ text: ctx.parse(JSON.stringify(value)) }),\n" +
	"    async: async (value, ctx) => ({ text: await ctx.parse(JSON.stringify(value)) }),\n" +
	"    stream: (value, ctx) => ({ text: ctx.parse(JSON.stringify(value)) }),\n" +
	"  },\n" +
	"  serialize: (node, ctx) => 'JSON.parse(' + ctx.serialize(node.text) + ')',\n" +
	"  deserialize: (node, ctx) => JSON.parse(ctx.deserialize(node.text) as string),\n" +
	"})\n" +
	"const values = [\n" +
	"  mark([1, [2, [], null], null, [], { a: [] }]),\n" +
	"  mark([]),\n" +
	"  { list: mark(['n', [[]]]), empty: mark([]), plain: [3, [], { b: [null] }] },\n" +
	"]\n" +
	"const lines: string[] = []\n" +
	"const line = (way: string, code: string) => lines.push(way + ' ' + code + ' ' + JSON.stringify((0, eval)(code)))\n" +
	"const run = async () => {\n" +
	"  for (const value of values) {\n" +
	"    line('sync', serialize(value, { plugins: [asJSON] }))\n" +
	"    line('async', await serializeAsync(value, { plugins: [asJSON] }))\n" +
	"    let code = ''\n" +
	"    crossSerializeStream(value, { plugins: [asJSON], onSerialize: (part) => (code += part), onError: (e) => { throw e } })\n" +
	"    ;(globalThis as any).$R = []\n" +
	"    line('stream', code)\n" +
	"  }\n" +
	"  globalThis.result = lines.join('\\n')\n" +
	"}\n" +
	"run().catch((e) => (globalThis.result = 'error ' + e))\n"

// plainArray finds the array of arraysPage that has no mark in seroval's own form, with or without
// a reference.
var plainArray = regexp.MustCompile(`plain:(\$R\[\d+\]=)?\[3,`)

// seroval tries its plugins on an object, but takes an array as an array before it asks them.
// The tifa plugin makes it ask them for arrays too, so the data of a page that is an array goes
// through the tifa.data adapter as its JSON text. Arrays the plugins do not take stay as they were.
func TestSSRSerializesArraysWithPlugins(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"arrays.ts": arraysPage})
	code, printed := buildPrinting(t, root, "arrays.ts")
	if strings.Contains(printed, "seroval") {
		t.Errorf("the build prints:\n%s", printed)
	}
	values := []string{`[1,[2,[],null],null,[],{"a":[]}]`, `[]`, `{"list":["n",[[]]],"empty":[],"plain":[3,[],{"b":[null]}]}`}
	lines := strings.Split(evalResult(t, code), "\n")
	if len(lines) != 3*len(values) {
		t.Fatalf("%d serializations:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for i, l := range lines {
		value := values[i/3]
		if !strings.HasSuffix(l, " "+value) {
			t.Errorf("serialization %d does not give back %s: %s", i, value, l)
		}
		// The marked arrays are JSON text, the plain one keeps seroval's own form.
		switch i / 3 {
		case 0:
			if !strings.Contains(l, `JSON.parse("[1,[2,[],null],null,[],{\"a\":[]}]")`) || strings.Contains(l, "{a:") {
				t.Errorf("serialization %d does not write the array as JSON text: %s", i, l)
			}
		case 1:
			if !strings.Contains(l, `JSON.parse("[]")`) {
				t.Errorf("serialization %d does not write the empty array as JSON text: %s", i, l)
			}
		case 2:
			if !strings.Contains(l, `JSON.parse("[\"n\",[[]]]")`) || !strings.Contains(l, `JSON.parse("[]")`) || !plainArray.MatchString(l) {
				t.Errorf("serialization %d does not write the marked arrays alone as JSON text: %s", i, l)
			}
		}
	}

	// A seroval with either first line in a form the plugin does not know keeps both, so it takes
	// arrays as arrays in each of its ways of writing, and the build names the function.
	for _, c := range []struct{ name, check, changed string }{
		{"parseObject", "\tif (Array.isArray(current)) return parseArray(ctx, depth, id, current);\n", "\tif (Array.isArray(current)) { return parseArray(ctx, depth, id, current); }\n"},
		{"parseObjectAsync", "\tif (Array.isArray(current)) return parseArray$1(ctx, depth, id, current);\n", "\tif (Array.isArray(current)) { return parseArray$1(ctx, depth, id, current); }\n"},
	} {
		changed := withChangedPackage(t, "seroval", filepath.Join("dist", "index.js"), func(source string) string {
			if strings.Count(source, c.check) != 1 {
				t.Fatalf("seroval does not have %q once", c.check)
			}
			return strings.Replace(source, c.check, c.changed, 1)
		})
		writeTree(t, changed, map[string]string{"arrays.ts": arraysPage})
		code, printed = buildPrinting(t, changed, "arrays.ts")
		if !strings.Contains(printed, "seroval 1.6.9: ") || !strings.Contains(printed, "has no "+c.name+" in the form") || !strings.Contains(printed, "takes an array as an array") {
			t.Errorf("%s: the build prints:\n%s", c.name, printed)
		}
		lines = strings.Split(evalResult(t, code), "\n")
		for i, l := range lines {
			if !strings.HasSuffix(l, " "+values[i/3]) {
				t.Errorf("%s: serialization %d does not give back %s: %s", c.name, i, values[i/3], l)
			}
			if i/3 == 0 && !strings.Contains(l, "{a:") {
				t.Errorf("%s: serialization %d of a seroval the plugin does not know writes the array as JSON text: %s", c.name, i, l)
			}
		}
	}
}

// serializesShortStrings reports whether seroval in a minified bundle serializes a string with
// JSON.stringify from a length of 2, which the minifier writes as a number or a variable.
func serializesShortStrings(code string) bool {
	m := regexp.MustCompile(`\.length>=([\w$]+)&&!\w+\.test\(\w+\)\)return JSON\.stringify`).FindStringSubmatch(code)
	if m == nil {
		return false
	}
	return m[1] == "2" || regexp.MustCompile(`[,;{(\s]`+regexp.QuoteMeta(m[1])+`=2[,;]`).MatchString(code)
}

// withReactDOM returns a copy of testdata/ssr whose react-dom is a copy of the test's, with old
// replaced by new in its string renderer. The other packages are the test's.
func withReactDOM(t *testing.T, old, new string) string {
	t.Helper()
	return withChangedReactDOM(t, func(source string) string {
		if strings.Count(source, old) != 1 {
			t.Fatalf("the string renderer of React DOM does not have %q once", old)
		}
		return strings.Replace(source, old, new, 1)
	})
}

// withChangedReactDOM returns a copy of testdata/ssr whose react-dom is a copy of the test's,
// with its string renderer changed by change. The other packages are the test's.
func withChangedReactDOM(t *testing.T, change func(string) string) string {
	t.Helper()
	return withChangedPackage(t, "react-dom", filepath.Join("cjs", "react-dom-server-legacy.browser.production.js"), change)
}

// withChangedPackage returns a copy of testdata/ssr whose package name is a copy of the test's,
// with its file changed by change. The other packages are the test's, but for those of the scope
// of a scoped name.
func withChangedPackage(t *testing.T, name, file string, change func(string) string) string {
	t.Helper()
	copied := copyTestdata(t, "ssr", false)
	modules := testModules(t)
	entries, err := os.ReadDir(modules)
	if err != nil {
		t.Fatal(err)
	}
	scope, _, scoped := strings.Cut(name, "/")
	for _, e := range entries {
		if e.Name() == name || scoped && e.Name() == scope || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(modules, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(copied, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(copied, "node_modules", e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	real, err := filepath.EvalSymlinks(filepath.Join(modules, name))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(copied, "node_modules", name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(target, os.DirFS(real)); err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(target, file)
	source, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, []byte(change(string(source))), 0o644); err != nil {
		t.Fatal(err)
	}
	return copied
}

// emotion takes the hash of @emotion/hash of every style it serializes, and the tifa plugin makes
// murmur2 ask __tifa_murmur2__ first, which ssr.Load defines to hash strings in Go. murmur2 hashes
// in JavaScript when __tifa_murmur2__ is not there or gives undefined, and when the plugin does not
// know the murmur2 of the package.
func TestSSRHashesEmotionStylesInGo(t *testing.T) {
	const page = "import hash from '@emotion/hash'\n" +
		"const styles = ['', 'go', 'color:red;', 'x'.repeat(4099)]\n" +
		"const before = styles.map(hash)\n" +
		"globalThis.__tifa_murmur2__ = (s) => (s === 'go' ? 'go!' : void 0)\n" +
		"globalThis.result = before.concat(styles.map(hash)).join(' ')\n"
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{"hash.ts": page})
	code, printed := buildPrinting(t, root, "hash.ts")
	if strings.Contains(printed, "emotion") {
		t.Errorf("the build prints:\n%s", printed)
	}
	hashed := strings.Fields(evalResult(t, code))
	changed := withChangedPackage(t, "@emotion/hash", filepath.Join("dist", "emotion-hash.esm.js"), func(source string) string {
		const mix = "h ^= h >>> 13;"
		if strings.Count(source, mix) != 1 {
			t.Fatalf("@emotion/hash does not have %q once", mix)
		}
		return strings.Replace(source, mix, "h = h ^ h >>> 13;", 1)
	})
	writeTree(t, changed, map[string]string{"hash.ts": page})
	code, printed = buildPrinting(t, changed, "hash.ts")
	if !strings.Contains(printed, "@emotion/hash 0.9.2: ") || !strings.Contains(printed, "has no murmur2 the tifa plugin knows") {
		t.Errorf("the build prints:\n%s", printed)
	}
	inJS := strings.Fields(evalResult(t, code))
	if len(hashed) != 8 || len(inJS) != 8 {
		t.Fatalf("hashes %q with the change, %q without it", hashed, inJS)
	}
	for i, want := range append(inJS[:4:4], inJS[4], "go!", inJS[6], inJS[7]) {
		if hashed[i] != want {
			t.Errorf("hash %d is %q, want %q", i, hashed[i], want)
		}
	}
}

func TestSSRTrimmedReactKeepsEveryPublicExport(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	const want = "renderToReadableStream,renderToStaticMarkup,renderToString,resume,version"
	for _, entry := range []string{"exports-namespace.ts", "exports-default.ts"} {
		code, _ := buildScript(t, root, entry)
		if got := evalResult(t, code); got != want {
			t.Errorf("%s: exports %q, want %q", entry, got, want)
		}
	}
}

func TestSSRRejectsNodeAPIsInPageCode(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	for _, entry := range []string{"node-prefixed.ts", "node-bare.ts"} {
		_, _, err := script(testdata(t), root, entry, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "Node APIs are not supported in page code") {
			t.Errorf("%s: err = %v, want Node API rejection", entry, err)
		}
	}
}

// A dependency that still loads a Node module in the bundle gets a warning at the place it does,
// with its package, whether it requires the module or imports it. The build goes on: the module
// may never load on the server. A require that a typeof require test guards gets none: QuickJS
// takes the other branch.
func TestSSRWarnsOfNodeAPIsLeftInDependencies(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/uses-fs/package.json":    `{"name":"uses-fs","main":"index.js"}`,
		"node_modules/uses-fs/index.js":        `const fs = require('fs'); module.exports = (p) => fs.readFileSync(p)`,
		"node_modules/esm-fs/package.json":     `{"name":"esm-fs","type":"module","main":"index.js"}`,
		"node_modules/esm-fs/index.js":         "import { readFileSync } from 'node:fs'\nexport const read = (p) => readFileSync(p)\n",
		"esm-fs.ts":                            "import { read } from 'esm-fs'\nglobalThis.result = String(read)\n",
		"node_modules/guarded-fs/package.json": `{"name":"guarded-fs","main":"index.js"}`,
		"node_modules/guarded-fs/index.js": "module.exports = function (name) {\n" +
			"  return typeof require === 'function' ? require('fs').existsSync(name) : 'safe'\n}\n",
		"guarded-fs.ts": "import exists from 'guarded-fs'\nglobalThis.result = exists('x')\n",
	})
	want := filepath.Join("node_modules", "uses-fs", "index.js") + `:1:12 (uses-fs): require("fs") loads a Node module`
	if _, got := buildPrinting(t, root, "uses-fs.ts"); !strings.Contains(got, want) {
		t.Errorf("uses-fs.ts: the build prints:\n%s\nwant %s", got, want)
	}
	if _, got := buildPrinting(t, root, "esm-fs.ts"); !strings.Contains(got, `require("node:fs") loads a Node module imported by esm-fs`) {
		t.Errorf("esm-fs.ts: the build prints:\n%s\nwant the import of node:fs", got)
	}
	code, got := buildPrinting(t, root, "guarded-fs.ts")
	if strings.Contains(got, "guarded-fs") {
		t.Errorf("guarded-fs.ts: the build prints:\n%s", got)
	}
	if got := evalResult(t, code); got != "safe" {
		t.Errorf("guarded require('fs') gives %q", got)
	}
}

// The SSR bundle takes the main files of packages and not their browser files. A browser file
// often takes what the browser has, as abort-controller takes AbortController from self, and
// QuickJS has none of it: the render would fail where the main file works. Conditional exports
// leave the browser condition out too, a package may export there code that renders only in the
// browser.
func TestSSRKeepsTheMainFilesOfPackages(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/two-mains/package.json":   `{"name":"two-mains","main":"index.js","browser":"browser.js"}`,
		"node_modules/two-mains/index.js":       "module.exports = class { get name() { return 'main' } }\n",
		"node_modules/two-mains/browser.js":     "const { Thing } = typeof self !== 'undefined' ? self : undefined\nmodule.exports = Thing\n",
		"node_modules/conditional/package.json": `{"name":"conditional","exports":{".":{"browser":"./client.js","default":"./server.js"}}}`,
		"node_modules/conditional/client.js":    "export default 'client'\n",
		"node_modules/conditional/server.js":    "export default 'server'\n",
		"mains.ts": "import Thing from 'two-mains'\nimport which from 'conditional'\n" +
			"globalThis.result = new Thing().name + ',' + which\n",
	})
	code, _ := buildPrinting(t, root, "mains.ts")
	if got := evalResult(t, code); got != "main,server" {
		t.Errorf("result = %q, want main,server", got)
	}
}

func TestSSRDefinesProductionGlobals(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	code, _ := buildScript(t, root, "define.ts")
	if got := evalResult(t, code); got != "production:true" {
		t.Fatalf("result = %q, want production:true", got)
	}
	if !strings.HasPrefix(code, "(function(){") || strings.Contains(code, "\n  ") {
		t.Errorf("SSR bundle is not a minified IIFE: %.40s", code)
	}
}

func TestSSRSelectsCoreJSPropertyState(t *testing.T) {
	root := copyTestdata(t, "ssr", true)
	code, modules := buildScript(t, root, "url.ts")
	if hasModule(modules, "core-js/internals/weak-map-basic-detection.js") {
		t.Error("core-js WeakMap detection is still bundled")
	}
	if !slices.Contains(modules, "\x00tifa-core-js-property-state") {
		t.Errorf("core-js property state adapter not applied: %v", modules)
	}
	if got := evalResult(t, code); got != "SH-1042" {
		t.Fatalf("URL polyfill result = %q", got)
	}
}

// fakeCoreJS installs a core-js of version at dir whose internal state module is state.
func fakeCoreJS(t *testing.T, root, dir, version, state string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		dir + "/package.json":                          `{"name":"core-js","version":"` + version + `"}`,
		dir + "/internals/internal-state.js":           state,
		dir + "/internals/weak-map-basic-detection.js": "module.exports = true",
	})
}

const coreJSState = "module.exports = require('../internals/weak-map-basic-detection') ? 'weak map' : 'properties'"

// The core-js adapter does not depend on the version: as long as the internal state module loads
// the WeakMap detection, it gets the property-backed state.
func TestSSRAdaptsCoreJSOfOtherVersions(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	fakeCoreJS(t, root, "node_modules/core-js", "3.99.0", coreJSState)
	var output strings.Builder
	_, modules, err := script(testdata(t), root, "core-js-state.ts", &output)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(modules, "\x00tifa-core-js-property-state") {
		t.Errorf("core-js property state adapter not applied: %v", modules)
	}
	if strings.Contains(output.String(), "core-js") {
		t.Errorf("warning for an adapted core-js:\n%s", output.String())
	}
}

// A core-js whose internal state no longer loads the WeakMap detection builds as it is, and the
// build says that the adapter did not apply.
func TestSSRWarnsWhenTheCoreJSAdapterDoesNotApply(t *testing.T) {
	for name, dir := range map[string]string{"installed": "node_modules/core-js", "linked": "vendor/core-js"} {
		t.Run(name, func(t *testing.T) {
			root := copyTestdata(t, "ssr", false)
			if name == "linked" {
				linkCoreJS(t, root)
			}
			fakeCoreJS(t, root, dir, "4.0.0", "module.exports = 'new state'")
			var output strings.Builder
			if _, _, err := script(testdata(t), root, "core-js-state.ts", &output); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "core-js 4.0.0") || !strings.Contains(output.String(), "WeakMap") {
				t.Errorf("no warning about the core-js adapter:\n%s", output.String())
			}
		})
	}
}

// fakeReactDOM installs a copy of the react-dom of testdata with version and server.edge.js
// changed, next to the react of testdata.
func fakeReactDOM(t *testing.T, root, version, entry string) {
	t.Helper()
	real, err := filepath.EvalSymlinks(filepath.Join(testModules(t), "react-dom"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "node_modules", "react-dom")
	if err := os.CopyFS(dir, os.DirFS(real)); err != nil {
		t.Fatal(err)
	}
	pkg, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkg = regexp.MustCompile(`"version": "[^"]+"`).ReplaceAll(pkg, []byte(`"version": "`+version+`"`))
	writeTree(t, root, map[string]string{"node_modules/react-dom/package.json": string(pkg)})
	if entry != "" {
		writeTree(t, root, map[string]string{"node_modules/react-dom/server.edge.js": entry})
	}
	if err := os.Symlink(filepath.Join(testModules(t), "react"), filepath.Join(root, "node_modules", "react")); err != nil {
		t.Fatal(err)
	}
}

// The renderer trimming reads the form of react-dom/server.edge.js, not its version or bytes.
func TestSSRTrimsReactDOMOfOtherVersions(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(testModules(t), "react-dom", "server.edge.js"))
	if err != nil {
		t.Fatal(err)
	}
	root := copyTestdata(t, "ssr", false)
	fakeReactDOM(t, root, "19.4.0", "// another release\n"+string(source))
	var output strings.Builder
	code, modules, err := script(testdata(t), root, "react-dom-server.ts", &output)
	if err != nil {
		t.Fatal(err)
	}
	if hasModule(modules, "react-dom-server.edge.production.js") || !hasModule(modules, "react-dom-server-legacy.browser.production.js") {
		t.Errorf("not trimmed to the string renderer: %v", modules)
	}
	if got := evalResult(t, code); got != "function" {
		t.Errorf("result = %q", got)
	}
	if strings.Contains(output.String(), "react-dom") {
		t.Errorf("warning for a trimmed react-dom:\n%s", output.String())
	}
}

// A server.edge.js of a form the trimming does not know is bundled as it is, with both renderers,
// and the build says so. The stream renderer needs the host patches to load, which the bundle
// has, so it still runs in QuickJS.
func TestSSRKeepsAReactDOMItCannotTrim(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	fakeReactDOM(t, root, "20.0.0", "'use strict';\n"+
		"const l = require('./cjs/react-dom-server-legacy.browser.production.js');\n"+
		"const b = require('./cjs/react-dom-server.edge.production.js');\n"+
		"Object.assign(exports, { renderToString: l.renderToString, renderToReadableStream: b.renderToReadableStream });\n")
	var output strings.Builder
	code, modules, err := script(testdata(t), root, "react-dom-server.ts", &output)
	if err != nil {
		t.Fatal(err)
	}
	if !hasModule(modules, "react-dom-server.edge.production.js") || !hasModule(modules, "react-dom-server-legacy.browser.production.js") {
		t.Errorf("not both renderers: %v", modules)
	}
	host, err := os.ReadFile(filepath.Join("..", "..", "js", "runtime", "host.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got := evalResult(t, string(host)+"\n"+code); got != "function" {
		t.Errorf("result = %q", got)
	}
	if !strings.Contains(output.String(), "react-dom 20.0.0") {
		t.Errorf("no warning about the untrimmed react-dom:\n%s", output.String())
	}
}

// A require whose module name is known only at run time stays in the bundle, and QuickJS has no
// require: the build warns of it at its place, with its package.
func TestSSRWarnsOfRequireLeftInDependencies(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/dynamic-reader/package.json": `{"name":"dynamic-reader","main":"index.cjs"}`,
		"node_modules/dynamic-reader/index.cjs":    "module.exports = function (name) {\n  return require(name).sep\n}\n",
		"dynamic.ts":                               "import read from 'dynamic-reader'\nglobalThis.result = read('node:path')\n",
	})
	want := filepath.Join("node_modules", "dynamic-reader", "index.cjs") + ":2:10 (dynamic-reader): require loads a module"
	if _, got := buildPrinting(t, root, "dynamic.ts"); !strings.Contains(got, want) {
		t.Fatalf("the build prints:\n%s\nwant %s", got, want)
	}
}

// A require declared in a block is bound only in that block: a require after it is still the
// host's. The column counts UTF-16 units, as in the source map.
func TestSSRWarnsOfRequireOutsideTheBlockThatDeclaresIt(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/dynamic-reader/package.json": `{"name":"dynamic-reader","main":"index.cjs"}`,
		"node_modules/dynamic-reader/index.cjs": "module.exports = function (name) {\n" +
			"  { const require = value => value; globalThis.label = require('本地标签') }\n" +
			"  try { name.x() } catch (require) {}\n" +
			"  return require(name).sep\n}\n",
		"dynamic.ts": "import read from 'dynamic-reader'\nglobalThis.result = read('node:path')\n",
	})
	_, got := buildPrinting(t, root, "dynamic.ts")
	if want := filepath.Join("dynamic-reader", "index.cjs") + ":4:10"; !strings.Contains(got, want) || strings.Count(got, "index.cjs:") != 1 {
		t.Fatalf("the build prints:\n%s\nwant one warning at %s", got, want)
	}
}

// No warning for a require the code never reaches in QuickJS: one in dead code such as the branch
// of an if (false), one that a typeof require test guards, as UMD and isomorphic packages write
// it, and one the module declares itself, as a bundle made by browserify does.
func TestSSRDoesNotWarnOfRequireThatNeverRuns(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	writeTree(t, root, map[string]string{
		"node_modules/guarded/package.json": `{"name":"guarded","main":"index.cjs"}`,
		"node_modules/guarded/index.cjs": "module.exports = function (name) {\n" +
			"  if (false) return require(name).sep\n" +
			"  var a = typeof require === 'function' ? require(name) : 'a'\n" +
			"  var b = 'b'\n  if (typeof require !== 'undefined') b = require(name)\n" +
			"  var c = (typeof require == 'function' && require(name)) || 'c'\n" +
			"  var d = typeof require === 'undefined' || require(name) ? 'd' : 'x'\n" +
			"  var e = !(typeof require === 'function') ? 'e' : require(name)\n" +
			"  var g = 'function' === typeof module && typeof require == 'function' ? require(name) : 'g'\n" +
			"  return a + b + c + d + e + early(name) + g + own\n}\n" +
			"function early(name) {\n  if (typeof require === 'undefined') return 'f'\n  console.log(name)\n  return require(name)\n}\n" +
			"var load = (function (require) { return function (n) { return require(n) } })(function (n) { return n + '!' })\n" +
			"function local(name) { var require = function (n) { return n + '?' }; return require(name) }\n" +
			"var own = load('h') + local('i')\n",
		"guarded.ts": "import read from 'guarded'\nglobalThis.result = read('node:path')\n",
	})
	code, output := buildPrinting(t, root, "guarded.ts")
	if strings.Contains(output, "guarded") {
		t.Errorf("the build prints:\n%s", output)
	}
	if got := evalResult(t, code); got != "abcdefgh!i?" {
		t.Fatalf("result = %q", got)
	}
}

// A require next to a typeof test that does not guard it still gets a warning.
func TestSSRWarnsOfRequireTheTypeofTestDoesNotGuard(t *testing.T) {
	for name, body := range map[string]string{
		"other variable": "  return typeof define === 'function' ? require(name) : 'x'\n",
		"after the test": "  if (typeof require === 'function') console.log(name)\n  return require(name)\n",
		"wrong branch":   "  return typeof require === 'undefined' ? require(name) : 'x'\n",
		"wrong operator": "  return typeof require == 'function' || require(name)\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := copyTestdata(t, "ssr", false)
			writeTree(t, root, map[string]string{
				"node_modules/unguarded/package.json": `{"name":"unguarded","main":"index.cjs"}`,
				"node_modules/unguarded/index.cjs":    "module.exports = function (name) {\n" + body + "}\n",
				"unguarded.ts":                        "import read from 'unguarded'\nglobalThis.result = read('node:path')\n",
			})
			if _, got := buildPrinting(t, root, "unguarded.ts"); !strings.Contains(got, filepath.Join("unguarded", "index.cjs")+":") {
				t.Fatalf("the build prints:\n%s\nwant the place of the require", got)
			}
		})
	}
}

func TestSSRStubsClientOnlyModules(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	code, modules := buildScript(t, root, "client-only.tsx")
	if hasModule(modules, "widget.client.tsx") {
		t.Fatal("client-only source bundled into SSR")
	}
	if got := evalResult(t, code); got != "Client-only module invoked during SSR" {
		t.Fatalf("result = %q", got)
	}
}

func TestSSRReportsMissingEntry(t *testing.T) {
	root := copyTestdata(t, "ssr", false)
	_, _, err := script(testdata(t), root, "missing.tsx", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "missing.tsx") {
		t.Fatalf("err = %v, want error naming the entry", err)
	}
}

// linkCoreJS installs core-js the way a vendored or linked package would be:
// node_modules/core-js is a symlink to a directory outside node_modules.
func linkCoreJS(t *testing.T, root string) string {
	t.Helper()
	vendor := filepath.Join(root, "vendor", "core-js")
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vendor, filepath.Join(root, "node_modules", "core-js")); err != nil {
		t.Fatal(err)
	}
	return vendor
}

func TestSSRAdaptsSymlinkedCoreJS(t *testing.T) {
	real, err := filepath.EvalSymlinks(filepath.Join(testModules(t), "core-js"))
	if err != nil {
		t.Fatal(err)
	}
	root := copyTestdata(t, "ssr", false)
	vendor := linkCoreJS(t, root)
	if err := os.CopyFS(filepath.Join(vendor, "internals"), os.DirFS(filepath.Join(real, "internals"))); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(real, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"vendor/core-js/package.json": string(data)})
	_, modules := buildScript(t, root, "core-js-state.ts")
	if !slices.Contains(modules, "\x00tifa-core-js-property-state") {
		t.Fatal("core-js property state adapter not applied to a symlinked core-js")
	}
	if hasModule(modules, "core-js/internals/weak-map-basic-detection.js") {
		t.Fatal("core-js WeakMap detection is still bundled")
	}
}

// manyRequires builds a bundle of a package with n requires and returns what the build printed
// and how long it took.
func manyRequires(t *testing.T, n int) (string, time.Duration) {
	t.Helper()
	root := copyTestdata(t, "ssr", false)
	var source strings.Builder
	for i := range n {
		fmt.Fprintf(&source, "exports.f%d = () => require(globalThis.n%d)\n", i, i)
	}
	writeTree(t, root, map[string]string{
		"node_modules/many/package.json": `{"name":"many","main":"index.js"}`,
		"node_modules/many/index.js":     source.String(),
		"many.ts":                        "import * as many from 'many'\nglobalThis.result = Object.keys(many).length\n",
	})
	start := time.Now()
	_, output := buildPrinting(t, root, "many.ts")
	return output, time.Since(start)
}

// Node may exit before it wrote all it printed to a pipe, and a long warning lost its end.
func TestSSRPrintsEveryWarning(t *testing.T) {
	const n = 20000
	output, _ := manyRequires(t, n)
	if got := strings.Count(output, " (many): require loads a module"); got != n {
		t.Errorf("%d warnings, want %d", got, n)
	}
	if !strings.Contains(output, fmt.Sprintf("node_modules/many/index.js:%d:", n)) {
		t.Errorf("the last require is not placed in its line:\n%.2000s", output)
	}
}

// The build places each require it warns of in the sources in about the same time, however many
// there are.
func TestSSRWarnsOfManyRequiresInTime(t *testing.T) {
	_, elapsed := manyRequires(t, 20000)
	t.Logf("the build took %s", elapsed)
	if elapsed > 3*time.Second {
		t.Errorf("the build took %s", elapsed)
	}
}
