package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// jsxPrelude starts a page that makes elements with explicit calls of jsx and jsxs of React's JSX
// runtime and describes each element in every way a program can see it: the order, values and
// attributes of its fields and of its props, the prototypes, whether it is frozen, the reads and
// calls that happened while it was made, and the error. The same page runs on the JSX runtime
// the tifa plugin writes as objects and on one it does not know, which keeps the calls.
const jsxPrelude = `import { jsx, jsxs, Fragment } from 'react/jsx-runtime'

let events: string[] = []
const note = <T,>(s: string, v: T): T => (events.push(s), v)
function Comp(): null {
  return null
}
const base: any = { b1: 'x', b2: 2 }
const ref = { current: null }
const name = 'dyn'
const item = { id: 'item-1' }
const f = () => 'f-key'
const n = 7
const fake = { jsx: (t: unknown, p: unknown) => ({ custom: true, t, p }) }
// The keys that names pass to jsx: the ones jsx turns into a string, the ones it leaves out and
// objects that turn into one by calling code.
const keys: Record<string, any> = {
  str: 'key-str',
  empty: '',
  num: 3,
  zero: 0,
  big: 1e21,
  neg: -2,
  undef: undefined,
  nul: null,
  bool: false,
  obj: { toString: () => (note('key.toString', 'from-object')) },
  arr: ['a', 'b'],
}

const SYM = Symbol.for('react.transitional.element')
function show(v: any, depth = 0): string {
  if (typeof v === 'symbol') return v.toString()
  if (typeof v === 'string') return JSON.stringify(v)
  if (typeof v === 'function') return 'function'
  if (v !== null && typeof v === 'object') {
    if (v.$$typeof === SYM) return depth > 3 ? '{element}' : 'element ' + describe(v, depth + 1)
    return Array.isArray(v) ? '[' + v.map((x) => show(x, depth + 1)).join() + ']' : '{' + Reflect.ownKeys(v).map(String).join() + '}'
  }
  return String(v)
}
function fields(o: any, depth: number): string {
  return Reflect.ownKeys(o)
    .map((k) => {
      const d: any = Object.getOwnPropertyDescriptor(o, k)
      const flags = (d.writable ? 'w' : '-') + (d.enumerable ? 'e' : '-') + (d.configurable ? 'c' : '-')
      return String(k) + ':' + ('value' in d ? show(d.value, depth) : 'accessor') + ':' + flags
    })
    .join(' | ')
}
function describe(el: any, depth = 0): string {
  const proto = (o: any) => (Object.getPrototypeOf(o) === Object.prototype ? 'Object.prototype' : Object.getPrototypeOf(o) === null ? 'null' : 'other')
  return [
    fields(el, depth),
    'proto ' + proto(el),
    'props proto ' + proto(el.props),
    'frozen ' + Object.isFrozen(el) + ' ' + Object.isFrozen(el.props),
    'extensible ' + Object.isExtensible(el) + ' ' + Object.isExtensible(el.props),
    'props ' + fields(el.props, depth),
    'same props ' + (el.props === base),
  ].join(' ; ')
}

const out: string[] = []
const add = (label: string, make: () => any) => {
  events = []
  let result: string
  try {
    const el = make()
    result = el && el.$$typeof === SYM ? describe(el) : show(el) + ' ' + JSON.stringify(el)
  } catch (e: any) {
    result = 'throws ' + (e && e.name)
  }
  out.push(label + ' => ' + result + ' ; events ' + events.join(','))
}
`

const jsxEpilogue = `globalThis.result = JSON.stringify(out)
`

// jsxTypes and jsxKeys are crossed for the cases that jsx and jsxs make elements of: three kinds
// of type, and every kind of third argument that is a literal or a name.
var jsxTypes = []string{"'div'", "Comp", "Fragment"}
var jsxLiteralKeys = []string{"'k'", "''", "3", "0", "1.5", "1e21", "'a\\u2028b'"}
var jsxNameKeys = []string{"str", "empty", "num", "zero", "big", "neg", "undef", "nul", "bool", "obj", "arr", "missing"}

// jsxInlinable is the cases in which the tifa plugin writes the call as an object. Each call
// of jsx or jsxs in it counts, also those inside the config of another.
func jsxInlinable() []string {
	var cases []string
	for _, ty := range jsxTypes {
		label := strings.ReplaceAll(ty, "'", "")
		cases = append(cases,
			fmt.Sprintf("add('jsx %s', () => jsx(%s, { a: 1, children: 'x' }))", label, ty),
			fmt.Sprintf("add('jsxs %s', () => jsxs(%s, { children: [1, 'two', null] }))", label, ty),
			fmt.Sprintf("add('empty %s', () => jsx(%s, {}))", label, ty),
		)
		for _, k := range jsxLiteralKeys {
			cases = append(cases, fmt.Sprintf("add('literal key %s %s', () => jsx(%s, { a: 1 }, %s))", label, strings.ReplaceAll(k, "'", ""), ty, k))
		}
		for _, k := range jsxNameKeys {
			cases = append(cases, fmt.Sprintf("add('name key %s %s', () => { const k: any = keys.%s; return jsx(%s, { a: 1 }, k) })", label, k, k, ty))
		}
	}
	return append(cases,
		"add('nested', () => jsx('ul', { className: 'l', children: [jsx('li', { children: 1 }, 'a'), jsx('li', { children: jsx('b', {}) }, 'b')] }))",
		"add('accessors and methods', () => jsx('div', { get g() { return note('get g', 1) }, set s(v: number) { note('set s', v) }, m() { return 1 }, async am() {} }))",
		"add('literal names', () => jsx('div', { 1: 'one', 'data-x': 2, 'a b': 3, 0: 'zero' } as any))",
		"add('shorthand', () => { const a = 1; const b = 'bee'; return jsx('div', { a, b }) })",
		"add('values with effects', () => jsx(note('type', 'div'), { a: note('a', 1), b: note('b', 2) }, 'k'))",
		"add('name key with effects in the values', () => { const k: any = keys.str; return jsx(note('type', 'div'), { a: note('a', 1), b: note('b', 2) }, k) })",
		"add('object key', () => { const k: any = keys.obj; return jsx('div', { a: 1 }, k) })",
		"add('a function and an arrow as values', () => jsx('button', { onClick: () => 1, onFocus: function () { return 2 } }))",
		"add('base as a value', () => jsx('div', { data: base, n: n }))",
		"add('a getter reads the key', () => { const k: any = keys.str; return jsx('div', { get a() { return k } }, k) })",
		"add('fake runtime', () => fake.jsx('div', { a: 1 }))",
	)
}

// jsxPlain is the cases in which it keeps the call, none of them with a call of jsx in a value.
var jsxPlain = []string{
	"add('spread', () => jsx('div', { ...base }))",
	"add('spread after', () => jsx('div', { a: 1, ...base }))",
	"add('spread before', () => jsx('div', { ...base, a: 1 }))",
	"add('spread with a key', () => jsx('div', { ...{ key: 'spread-key', a: 1 } }))",
	"add('spread with a key and a third argument', () => jsx('div', { ...{ key: 'spread-key', a: 1 } }, 'third'))",
	"add('spread with a ref', () => jsx('div', { ...{ ref, a: 1 } }))",
	"add('spread of nothing', () => jsx('div', { ...(base.missing as any), ...base.nothing }))",
	"add('spread of a string', () => jsx('div', { ...('ab' as any) }))",
	"add('spread type', () => jsx(...(['div', { a: 1 }] as [string, object])))",
	"add('ref', () => jsx('div', { ref, a: 1 }))",
	"add('ref string', () => jsx('div', { 'ref': ref, a: 1 } as any))",
	"add('ref undefined', () => jsx('div', { ref: undefined, a: 1 }))",
	"add('ref null', () => jsx('div', { a: 1, ref: null }))",
	"add('ref shorthand', () => jsx('div', { ref }))",
	"add('ref getter', () => jsx('div', { get ref() { return note('get ref', ref) } }))",
	"add('key in the config', () => jsx('div', { key: 'cfg-key', a: 1 } as any))",
	"add('key undefined in the config', () => jsx('div', { key: undefined, a: 1 } as any))",
	"add('key null in the config', () => jsx('div', { key: null, a: 1 } as any))",
	"add('key number in the config', () => jsx('div', { key: 4, a: 1 } as any))",
	"add('key string name', () => jsx('div', { 'key': 'q', a: 1 } as any))",
	"add('key getter', () => jsx('div', { get key() { return note('get key', 'g-key') }, a: 1 } as any))",
	"add('key in the config and a third argument', () => jsx('div', { key: 'cfg-key' } as any, 'third'))",
	"add('key undefined in the config and a third argument', () => jsx('div', { key: undefined } as any, 'third'))",
	"add('computed name', () => jsx('div', { [name]: 1 }))",
	"add('computed key', () => jsx('div', { ['key']: 'ck', a: 1 } as any))",
	"add('computed ref', () => jsx('div', { ['ref']: ref, a: 1 } as any))",
	"add('computed __proto__', () => jsx('div', { ['__proto__']: { x: 1 }, a: 1 } as any))",
	"add('__proto__', () => jsx('div', { __proto__: base, a: 1 } as any))",
	"add('__proto__ string', () => jsx('div', { '__proto__': base, a: 1 } as any))",
	"add('__proto__ null', () => jsx('div', { __proto__: null, a: 1 } as any))",
	"add('__proto__ shorthand', () => { const __proto__ = { z: 1 }; return jsx('div', { __proto__, a: 1 } as any) })",
	"add('key from a member', () => jsx('div', { a: 1 }, item.id))",
	"add('key from a call', () => jsx('div', { a: 1 }, f()))",
	"add('key from a template', () => jsx('div', { a: 1 }, `k${n}`))",
	"add('key from a sum', () => jsx('div', { a: 1 }, 'a' + item.id))",
	"add('key undefined', () => jsx('div', { a: 1 }, undefined))",
	"add('key null', () => jsx('div', { a: 1 }, null as any))",
	"add('key negative', () => jsx('div', { a: 1 }, -1))",
	"add('key true', () => jsx('div', { a: 1 }, true as any))",
	"add('key bigint', () => jsx('div', { a: 1 }, 5n as any))",
	"add('key from a call with effects', () => jsx(note('type', 'div'), { a: note('a', 1) }, note('key', 'ek')))",
	"add('config is a name', () => jsx('div', base))",
	"add('config is a call', () => jsx('div', note('cfg', { c: 1 })))",
	"add('config is shared', () => jsx(Comp, base))",
	"add('name key set by the config', () => { let k: any = 'before'; return jsx('div', { a: (k = 'after') }, k) })",
	"add('name key counted by the config', () => { let k: any = 1; return jsx('div', { a: k++ }, k) })",
	"add('name key set in a nested pattern', () => { let k: any = 'before'; let other: any; return jsx('div', { a: ([other, { k }] = [1, { k: 'after' }]) }, k) })",
	"add('four arguments', () => (jsx as any)('div', { a: 1 }, 'k', 'extra'))",
	"add('one argument', () => (jsx as any)('div'))",
	"add('no config', () => (jsx as any)('div', undefined, 'k'))",
	"add('null config', () => (jsx as any)('div', null))",
	"add('config is a string', () => (jsx as any)('div', 'text'))",
}

func jsxPage(cases []string) string {
	return jsxPrelude + strings.Join(cases, "\n") + "\n" + jsxEpilogue
}

var jsxCall = regexp.MustCompile(`\bjsxs?\(`)

// The tifa plugin writes a call of jsx or jsxs of React's JSX runtime as the object the call
// returns when it can tell what the call returns: the config is an object literal with no
// spread, no computed name and no key, ref or __proto__, and a third argument is a literal or a
// name. Every element then has the same fields in the same order with the same attributes, values
// and prototypes, and a program sees the same reads and calls, as with a JSX runtime the plugin
// does not know, which keeps the calls. The bundle holds one more object with $$typeof for each
// call it changed. Calls it cannot change stay, and give the same too.
func TestSSRWritesTheElementsOfJSXAsObjects(t *testing.T) {
	inlinable := jsxInlinable()
	var inlinableCalls int
	for _, c := range inlinable {
		inlinableCalls += len(jsxCall.FindAllString(c, -1))
	}
	// Two of the calls are of the fake runtime and of a call that is a name, not of React's.
	for _, c := range []struct {
		name  string
		cases []string
		calls int
	}{
		{"calls that can be objects", inlinable, inlinableCalls - 1},
		{"calls that stay", jsxPlain, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			page := jsxPage(c.cases)
			rootKnown := copyTestdata(t, "ssr", true)
			writeTree(t, rootKnown, map[string]string{"jsx.tsx": page})
			known, printed := buildPrinting(t, rootKnown, "jsx.tsx")
			if printed != "" {
				t.Errorf("the build prints:\n%s", printed)
			}
			rootUnknown := reactJSXRuntimeWithChanged(t, func(source string) string { return source + "// changed\n" })
			writeTree(t, rootUnknown, map[string]string{"jsx.tsx": page})
			unknown, printed := buildPrinting(t, rootUnknown, "jsx.tsx")
			if !strings.Contains(printed, "has a jsx the tifa plugin does not know") {
				t.Errorf("the build of a JSX runtime the plugin does not know prints:\n%s", printed)
			}
			if got := strings.Count(known, "$$typeof") - strings.Count(unknown, "$$typeof"); got != c.calls {
				t.Errorf("the bundle has %d objects with $$typeof more than with the calls, want %d", got, c.calls)
			}
			var want, got []string
			if err := json.Unmarshal([]byte(evalResult(t, unknown)), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(evalResult(t, known)), &got); err != nil {
				t.Fatal(err)
			}
			if len(want) != len(c.cases) || len(got) != len(want) {
				t.Fatalf("%d and %d outcomes of %d cases", len(want), len(got), len(c.cases))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%s\n got %s\nwant %s", c.cases[i], got[i], want[i])
				}
			}
		})
	}
}

// reactJSXRuntimeWithChanged returns a copy of testdata/ssr whose React has the text of its
// production JSX runtime changed by change.
func reactJSXRuntimeWithChanged(t *testing.T, change func(string) string) string {
	t.Helper()
	return withChangedPackage(t, "react", filepath.Join("cjs", "react-jsx-runtime.production.js"), change)
}

// A JSX runtime of which the plugin has not read the production function keeps the calls of jsx,
// and the build says so once: with the function changed, with the function replaced by another
// one and with a file of another size. The same runtime read again does not warn.
func TestSSRKeepsTheCallsOfAJSXRuntimeItDoesNotKnow(t *testing.T) {
	page := jsxPage([]string{
		"add('plain', () => jsx('div', { a: 1 }))",
		"add('key', () => { const k: any = keys.str; return jsx('div', { a: 1 }, k) })",
	})
	for _, c := range []struct {
		name   string
		change func(string) string
	}{
		{"a comment", func(s string) string { return s + "// changed\n" }},
		{"a changed function", func(s string) string {
			return strings.Replace(s, `void 0 !== maybeKey && (key = "" + maybeKey);`, `void 0 !== maybeKey && (key = "k" + maybeKey);`, 1)
		}},
		{"a ref that is not read", func(s string) string {
			return strings.Replace(s, "ref: void 0 !== config ? config : null,", "ref: null,", 1)
		}},
		{"a new field", func(s string) string {
			return strings.Replace(s, "props: maybeKey\n", "props: maybeKey, extra: 1\n", 1)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var seen bool
			root := reactJSXRuntimeWithChanged(t, func(s string) string {
				changed := c.change(s)
				seen = changed != s
				return changed
			})
			if !seen {
				t.Fatal("the change did not change the file")
			}
			writeTree(t, root, map[string]string{"jsx.tsx": page})
			code, printed := buildPrinting(t, root, "jsx.tsx")
			if n := strings.Count(printed, "has a jsx the tifa plugin does not know"); n != 1 || !strings.Contains(printed, "react 19.3.0") {
				t.Errorf("the build prints:\n%s", printed)
			}
			if !strings.Contains(printed, "Pages render the same, but take longer to render") {
				t.Errorf("the warning does not say what it costs:\n%s", printed)
			}
			// The same page with the JSX runtime the plugin knows has an object for each of the two calls.
			known := copyTestdata(t, "ssr", true)
			writeTree(t, known, map[string]string{"jsx.tsx": page})
			plain, _ := buildPrinting(t, known, "jsx.tsx")
			if got := strings.Count(plain, "$$typeof") - strings.Count(code, "$$typeof"); got != 2 {
				t.Errorf("the bundle has %d objects with $$typeof less than with the runtime the plugin knows, want 2", got)
			}
		})
	}
}

// The browser bundle keeps the calls of jsx: the SSR bundle is the only one the plugin changes.
func TestBrowserBundleKeepsTheCallsOfJSX(t *testing.T) {
	root := fakeApp(t, `["es2022"]`)
	writeTree(t, root, map[string]string{
		"tsconfig.json":   `{"compilerOptions":{"jsx":"react-jsx","module":"ESNext","moduleResolution":"bundler","target":"ES2022","strict":true}}`,
		".tifa/client.ts": "import { View } from '../app/view'\nconsole.log(View())\n",
		".tifa/server.ts": "declare const __TIFA_ASSETS__: string\nimport { View } from '../app/view'\n(globalThis as any).result = JSON.stringify(View()) + __TIFA_ASSETS__\n",
		"app/view.tsx":    "export function View() {\n  const k = Math.random() > 2 ? 'a' : 'b'\n  return [<div key={k} className=\"card\" id=\"one\"><span>text</span></div>, <p key=\"two\">more</p>]\n}\n",
	})
	buildApp(t, root)
	dist := filepath.Join(root, ".tifa", "dist")
	// The quotes the minifier picks differ, so the code is read with one kind.
	read := func(file string) string {
		code, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(code), "`", "\"")
	}
	object := regexp.MustCompile(`type:"(?:div|span|p)",key:`)
	client := 0
	assets, err := filepath.Glob(filepath.Join(dist, "assets", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range assets {
		code := read(file)
		if !strings.Contains(code, `className:"card"`) {
			continue
		}
		client++
		if m := object.FindString(code); m != "" {
			t.Errorf("%s has an element written as an object: %s", file, m)
		}
		if !regexp.MustCompile(`\.jsxs?\)?\("div"`).MatchString(code) && !regexp.MustCompile(`\w\("div",\{`).MatchString(code) {
			t.Errorf("%s has no call of jsx for the div", file)
		}
	}
	if client != 1 {
		t.Fatalf("%d browser scripts have the view, want 1", client)
	}
	// The SSR bundle of the same view has the span and the p. The bundler has made the key of the
	// div the expression that k stands for, which the plugin leaves a call.
	if code := read(filepath.Join(dist, "ssr.js")); len(object.FindAllString(code, -1)) != 2 {
		t.Errorf("the SSR bundle has %d elements written as objects, want 2", len(object.FindAllString(code, -1)))
	}
}

// inlined is what inlineElements of the tifa plugin gives for one source.
type inlined struct {
	Code                  string
	Sites, Inlined, Keyed int
	Parses                bool
}

// inlinedOnNode runs inlineElements on each of sources with the parser of Vite.
func inlinedOnNode(t *testing.T, sources []string) []inlined {
	t.Helper()
	testModules(t)
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	script := "const plugin = await import(process.argv[1])\n" +
		"const { parseAst } = await import('vite')\n" +
		"const parses = (code) => {\n  try {\n    return !!parseAst(code)\n  } catch {\n    return false\n  }\n}\n" +
		"const out = JSON.parse(process.argv[2]).map((source) => {\n" +
		"  const r = plugin.inlineElements(source, (code) => parseAst(code))\n" +
		"  return { Code: r.code, Sites: r.sites, Inlined: r.inlined, Keyed: r.keyed, Parses: parses(r.code) }\n" +
		"})\n" +
		"process.stdout.write(JSON.stringify(out))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("inlineElements: %v", err)
	}
	var results []inlined
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return results
}

// jsxBundle is the part of a bundle of rolldown that holds the JSX runtime, as the bundle has it
// before the minifier runs, with the form of its wrapper chosen by wrapper: a function of its
// own, or written in place of the import in the only module that has it.
func jsxBundle(wrapper, body string) string {
	runtime := `//#region node_modules/.pnpm/react@19.3.1/node_modules/react/cjs/react-jsx-runtime.production.js
/**
* @license React
* react-jsx-runtime.production.js
*/
var require_react_jsx_runtime_production = /* @__PURE__ */ __commonJSMin(((exports) => {
	exports.jsx = jsxProd;
	exports.jsxs = jsxProd;
}));
//#endregion
`
	switch wrapper {
	case "function":
		runtime += `//#region node_modules/.pnpm/react@19.3.1/node_modules/react/jsx-runtime.js
var require_jsx_runtime = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = require_react_jsx_runtime_production();
}));
//#endregion
//#region app/view.js
var import_jsx_runtime = __toESM(require_jsx_runtime());
`
	case "toESM":
		runtime += `//#region node_modules/.pnpm/react@19.3.1/node_modules/react/jsx-runtime.js
var require_jsx_runtime = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = require_react_jsx_runtime_production();
}));
//#endregion
//#region app/view.js
var import_jsx_runtime = __toESM(require_jsx_runtime(), 1);
`
	case "tifaV":
		runtime += `//#region node_modules/.pnpm/react@19.3.1/node_modules/react/jsx-runtime.js
var require_jsx_runtime = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = require_react_jsx_runtime_production();
}));
//#endregion
//#region app/view.js
var import_jsx_runtime = __tifaV$3(require_jsx_runtime());
`
	case "in place":
		runtime += `//#region app/view.js
var import_jsx_runtime = (/* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = require_react_jsx_runtime_production();
})))();
`
	case "lazy":
		runtime += `//#region node_modules/.pnpm/react@19.3.1/node_modules/react/jsx-runtime.js
var require_jsx_runtime = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = require_react_jsx_runtime_production();
}));
//#endregion
//#region app/view.js
var import_jsx_runtime;
var init_view = __esmMin((() => {
	import_jsx_runtime = require_jsx_runtime();
}));
`
	default:
		panic("wrapper " + wrapper)
	}
	return runtime + body
}

// inlineElements writes each call of jsx and jsxs it can as the object the call returns, for
// the forms rolldown writes the JSX runtime in, and keeps the others. Every line of the code stays
// where it was, and it still parses.
func TestPluginWritesTheCallsOfJSXAsObjects(t *testing.T) {
	call := func(args string) string { return "const el = (0, import_jsx_runtime.jsx)(" + args + ");\n" }
	for _, c := range []struct {
		name, source string
		sites, done  int
		keyed        int
		contains     []string
	}{
		{"typical", jsxBundle("function", call(`"div", { className: "a", children: "b" }`)), 1, 1, 0,
			[]string{`const el = ({ $$typeof: __tifaElement, type: "div", key: null, ref: null, props: { className: "a", children: "b" } });`}},
		{"jsxs", jsxBundle("function", "const el = (0, import_jsx_runtime.jsxs)(Comp, { children: [a, b] });\n"), 1, 1, 0,
			[]string{`({ $$typeof: __tifaElement, type: Comp, key: null, ref: null, props: { children: [a, b] } })`}},
		{"no parentheses around the callee", jsxBundle("function", "const el = import_jsx_runtime.jsx(Comp, {});\n"), 1, 1, 0,
			[]string{`({ $$typeof: __tifaElement, type: Comp, key: null, ref: null, props: {} })`}},
		{"string key", jsxBundle("function", call(`"li", { a: 1 }, "k"`)), 1, 1, 1, []string{`key: "k", ref: null`}},
		{"number key", jsxBundle("function", call(`"li", { a: 1 }, 1e21`)), 1, 1, 1, []string{`key: "1e+21", ref: null`}},
		{"name key", jsxBundle("function", call(`"li", { a: 1 }, id`)), 1, 1, 1, []string{`key: (void 0 !== id ? "" + id : null), ref: null`}},
		{"nested", jsxBundle("function", call(`"ul", { children: [(0, import_jsx_runtime.jsx)("li", {}, i), (0, import_jsx_runtime.jsx)("li", {})] }`)), 3, 3, 1,
			[]string{`type: "ul"`, `type: "li", key: (void 0 !== i ? "" + i : null)`, `type: "li", key: null`}},
		{"in a type", jsxBundle("function", call(`(0, import_jsx_runtime.jsx)("i", {}).type, { x: 1 }`)), 2, 2, 0, nil},
		{"toESM", jsxBundle("toESM", call(`"div", {}`)), 1, 1, 0, nil},
		{"plain namespace", jsxBundle("tifaV", call(`"div", {}`)), 1, 1, 0, nil},
		{"wrapper in place", jsxBundle("in place", call(`"div", {}`)), 1, 1, 0, nil},
		{"assigned when the module runs", jsxBundle("lazy", call(`"div", {}`)), 1, 1, 0, nil},
		{"spread", jsxBundle("function", call(`"div", { ...props }`)), 1, 0, 0, nil},
		{"spread after names", jsxBundle("function", call(`"div", { a, ...props }`)), 1, 0, 0, nil},
		{"ref", jsxBundle("function", call(`"div", { ref }`)), 1, 0, 0, nil},
		{"ref in quotes", jsxBundle("function", call(`"div", { "ref": r }`)), 1, 0, 0, nil},
		{"key", jsxBundle("function", call(`"div", { key: "a" }`)), 1, 0, 0, nil},
		{"computed name", jsxBundle("function", call(`"div", { [name]: 1 }`)), 1, 0, 0, nil},
		{"computed key", jsxBundle("function", call(`"div", { ["key"]: 1 }`)), 1, 0, 0, nil},
		{"__proto__", jsxBundle("function", call(`"div", { __proto__: p }`)), 1, 0, 0, nil},
		{"__proto__ in quotes", jsxBundle("function", call(`"div", { "__proto__": p }`)), 1, 0, 0, nil},
		{"config is a name", jsxBundle("function", call(`"div", props`)), 1, 0, 0, nil},
		{"member key", jsxBundle("function", call(`"div", {}, item.id`)), 1, 0, 0, nil},
		{"call key", jsxBundle("function", call(`"div", {}, f()`)), 1, 0, 0, nil},
		{"null key", jsxBundle("function", call(`"div", {}, null`)), 1, 0, 0, nil},
		{"negative key", jsxBundle("function", call(`"div", {}, -1`)), 1, 0, 0, nil},
		{"true key", jsxBundle("function", call(`"div", {}, true`)), 1, 0, 0, nil},
		{"key set by the config", jsxBundle("function", call(`"div", { a: (id = 2) }, id`)), 1, 0, 0, nil},
		{"key counted by the config", jsxBundle("function", call(`"div", { a: id++ }, id`)), 1, 0, 0, nil},
		{"key set in a pattern of the config", jsxBundle("function", call(`"div", { a: ([x, { id }] = y) }, id`)), 1, 0, 0, nil},
		{"key set in a function of the config", jsxBundle("function", call(`"div", { a: () => { id = 2; } }, id`)), 1, 0, 0, nil},
		{"another key set by the config", jsxBundle("function", call(`"div", { a: (other = 2) }, id`)), 1, 1, 1, nil},
		{"four arguments", jsxBundle("function", call(`"div", {}, "k", "extra"`)), 1, 0, 0, nil},
		{"one argument", jsxBundle("function", call(`"div"`)), 1, 0, 0, nil},
		{"spread arguments", jsxBundle("function", call(`...args`)), 1, 0, 0, nil},
		{"computed callee", jsxBundle("function", `const el = (0, import_jsx_runtime["jsx"])("div", {});`+"\n"), 0, 0, 0, nil},
		{"another export", jsxBundle("function", `const el = (0, import_jsx_runtime.jsxDEV)("div", {});`+"\n"), 0, 0, 0, nil},
		{"another object", jsxBundle("function", `const el = (0, other.jsx)("div", {});`+"\n"), 0, 0, 0, nil},
		{"a name given the runtime and another value",
			jsxBundle("function", "import_jsx_runtime = other;\n"+call(`"div", {}`)), 0, 0, 0, nil},
		{"a name changed by a pattern",
			jsxBundle("function", "[import_jsx_runtime] = other;\n"+call(`"div", {}`)), 0, 0, 0, nil},
		{"a name given a call of another function",
			jsxBundle("function", "var runtime = require_other();\nconst el = (0, runtime.jsx)(\"div\", {});\n"), 0, 0, 0, nil},
		{"lines of a call", jsxBundle("function", "const el = (0, import_jsx_runtime.jsx)(\n\t\"div\",\n\t{ a: 1 }\n\t,\n\t\"k\"\n);\n"), 1, 1, 1,
			[]string{"({ $$typeof: __tifaElement, type: \n\"div\"", "key: \"k\", ref: null, props: \n{ a: 1 }", " })\n\n\n;"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := inlinedOnNode(t, []string{c.source})[0]
			if got.Sites != c.sites || got.Inlined != c.done || got.Keyed != c.keyed {
				t.Errorf("sites, inlined, keyed = %d, %d, %d, want %d, %d, %d", got.Sites, got.Inlined, got.Keyed, c.sites, c.done, c.keyed)
			}
			if got.Inlined == 0 && got.Code != c.source {
				t.Errorf("the code changed:\n%s", got.Code)
			}
			if got.Inlined > 0 && !strings.Contains(got.Code, `var __tifaElement = Symbol.for("react.transitional.element"); `) {
				t.Errorf("the symbol is not taken:\n%s", got.Code)
			}
			if got.Inlined == 0 && strings.Contains(got.Code, "__tifaElement") {
				t.Errorf("the symbol is taken though no call changed:\n%s", got.Code)
			}
			for _, want := range c.contains {
				if !strings.Contains(got.Code, want) {
					t.Errorf("the code has no %q:\n%s", want, got.Code)
				}
			}
			if strings.Count(got.Code, "\n") != strings.Count(c.source, "\n") {
				t.Errorf("the code has %d lines, want %d", strings.Count(got.Code, "\n"), strings.Count(c.source, "\n"))
			}
			if !got.Parses && (c.name != "spread arguments") {
				t.Errorf("the code does not parse:\n%s", got.Code)
			}
		})
	}
}

// inlineElements finds the JSX runtime by what rolldown writes for the files of React, and does
// nothing where it does not find a file of React's production runtime, whose function is read by
// the caller.
func TestPluginLeavesOtherJSXRuntimesAlone(t *testing.T) {
	use := "var import_jsx_runtime = __toESM(require_jsx_runtime());\nconst el = (0, import_jsx_runtime.jsx)(\"div\", {});\n"
	wrapper := func(path string, requires string) string {
		return "//#region node_modules/" + path + "\nvar require_jsx_runtime = /* @__PURE__ */ __commonJSMin(((exports, module) => {\n\tmodule.exports = " + requires + "();\n}));\n//#endregion\n"
	}
	production := func(path string) string {
		return "//#region node_modules/" + path + "\nvar require_react_jsx_runtime_production = /* @__PURE__ */ __commonJSMin(((exports) => {\n\texports.jsx = jsxProd;\n}));\n//#endregion\n"
	}
	for _, c := range []struct {
		name, source string
		want         int
	}{
		{"react", production("react/cjs/react-jsx-runtime.production.js") + wrapper("react/jsx-runtime.js", "require_react_jsx_runtime_production") + use, 1},
		{"react in a package folder", production(".pnpm/react@19.3.1/node_modules/react/cjs/react-jsx-runtime.production.js") + wrapper(".pnpm/react@19.3.1/node_modules/react/jsx-runtime.js", "require_react_jsx_runtime_production") + use, 1},
		{"preact", production("preact/cjs/react-jsx-runtime.production.js") + wrapper("preact/jsx-runtime.js", "require_react_jsx_runtime_production") + use, 0},
		{"a file of another package named like it", production("other/react/cjs/react-jsx-runtime.production.js") + wrapper("react-fake/jsx-runtime.js", "require_react_jsx_runtime_production") + use, 0},
		{"emotion", wrapper("@emotion/react/jsx-runtime/dist/emotion-react-jsx-runtime.cjs.js", "require_emotion") + use, 0},
		{"development runtime", production("react/cjs/react-jsx-runtime.development.js") + wrapper("react/jsx-runtime.js", "require_react_jsx_runtime_development") + use, 0},
		{"profiling runtime", production("react/cjs/react-jsx-runtime.profiling.js") + wrapper("react/jsx-runtime.js", "require_react_jsx_runtime_production") + use, 0},
		{"wrapper without the production file", wrapper("react/jsx-runtime.js", "require_react_jsx_runtime_production") + use, 0},
		{"wrapper that requires another function", production("react/cjs/react-jsx-runtime.production.js") + wrapper("react/jsx-runtime.js", "require_other") + use, 0},
		{"no runtime", use, 0},
		{"the symbol name taken", production("react/cjs/react-jsx-runtime.production.js") + wrapper("react/jsx-runtime.js", "require_react_jsx_runtime_production") + "var __tifaElement = 1;\n" + use, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := inlinedOnNode(t, []string{c.source})[0]
			if got.Inlined != c.want {
				t.Errorf("%d calls changed, want %d:\n%s", got.Inlined, c.want, got.Code)
			}
			if c.want == 0 && got.Code != c.source {
				t.Errorf("the code changed:\n%s", got.Code)
			}
		})
	}
}

// The symbol goes after the directives and a hashbang, on the line of the first statement.
func TestPluginTakesTheSymbolOfElementsAfterTheDirectives(t *testing.T) {
	runtime := jsxBundle("function", "const el = (0, import_jsx_runtime.jsx)(\"div\", {});\n")
	for _, c := range []struct{ name, head string }{
		{"nothing", ""},
		{"a directive", "\"use strict\";\n"},
		{"two directives", "\"use strict\";\n'use asm';\n"},
		{"a hashbang", "#!/usr/bin/env node\n"},
		{"a hashbang and a directive", "#!/usr/bin/env node\n\"use strict\";\n"},
		{"a comment", "// first line\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := inlinedOnNode(t, []string{c.head + runtime})[0]
			if got.Inlined != 1 || !got.Parses {
				t.Fatalf("inlined %d, parses %v:\n%s", got.Inlined, got.Parses, got.Code)
			}
			if !strings.HasPrefix(got.Code, c.head) || strings.Index(got.Code, "var __tifaElement") < len(c.head) {
				t.Errorf("the symbol is not after the head:\n%s", got.Code)
			}
			if strings.Count(got.Code, "\n") != strings.Count(c.head+runtime, "\n") {
				t.Errorf("the lines moved:\n%s", got.Code)
			}
		})
	}
}
