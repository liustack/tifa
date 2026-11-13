package bundle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// hookWrappers is what reactHookWrappers of the tifa Vite plugin gives for a react.production.js.
type hookWrappers struct {
	Hooks    map[string]int
	Problems []string
}

// hookWrappersOnNode runs reactHookWrappers on each of sources with the parser of Vite.
func hookWrappersOnNode(t *testing.T, sources []string) []hookWrappers {
	t.Helper()
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	script := `
const plugin = await import(process.argv[1])
const { parseAst } = await import('vite')
const out = JSON.parse(process.argv[2]).map((source) => {
  const r = plugin.reactHookWrappers(parseAst(source), source)
  return { Hooks: Object.fromEntries(r.hooks), Problems: r.problems }
})
process.stdout.write(JSON.stringify(out))
`
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reactHookWrappers: %v", err)
	}
	var results []hookWrappers
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return results
}

// reactProduction is react.production.js of the React of the tests.
func reactProduction(t *testing.T) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(testModules(t), "react", "cjs", "react.production.js"))
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// React exports each hook as a function that hands its parameters to the dispatcher, and the
// plugin finds each with the number of its parameters.
func TestPluginReadsTheHookWrappersOfReact(t *testing.T) {
	got := hookWrappersOnNode(t, []string{reactProduction(t)})[0]
	if len(got.Problems) > 0 {
		t.Fatalf("problems: %v", got.Problems)
	}
	want := map[string]int{"useRef": 1, "useState": 1, "useContext": 1, "useMemo": 2, "useCallback": 2, "useEffect": 2, "useReducer": 3, "useId": 0, "useSyncExternalStore": 3}
	for name, n := range want {
		if got.Hooks[name] != n {
			t.Errorf("%s: %d parameters, want %d (all: %v)", name, got.Hooks[name], n, got.Hooks)
		}
	}
	if _, ok := got.Hooks["useDebugValue"]; ok {
		t.Errorf("useDebugValue, an empty function in React, counts as a wrapper")
	}
}

// A React whose dispatcher or wrappers can change, or that writes a wrapper otherwise, keeps every
// call of its hooks, or that hook's.
func TestPluginKnowsWhenTheHookWrappersOfReactDiffer(t *testing.T) {
	react := reactProduction(t)
	const useRef = "exports.useRef = function (initialValue) {\n  return ReactSharedInternals.H.useRef(initialValue);\n};"
	if !strings.Contains(react, useRef) {
		t.Fatalf("react.production.js has no %q", useRef)
	}
	cases := []struct {
		name, source string
		problem      string // empty when the file has no problem
		noRef        bool   // useRef is not a wrapper
	}{
		{name: "a wrapper that does more", source: strings.Replace(react, useRef, "exports.useRef = function (initialValue) {\n  initialValue = initialValue || null;\n  return ReactSharedInternals.H.useRef(initialValue);\n};", 1), noRef: true},
		{name: "a wrapper that passes its parameters in another order", source: strings.Replace(react, "return ReactSharedInternals.H.useMemo(create, deps);", "return ReactSharedInternals.H.useMemo(deps, create);", 1)},
		{name: "a wrapper set twice", source: react + "\n" + useRef + "\n", problem: "exports.useRef is set more than once"},
		{name: "the dispatcher's object set again", source: react + "\nReactSharedInternals = { H: null };\n", problem: "ReactSharedInternals"},
		{name: "exports handed on", source: react + "\nObject.freeze(exports);\n", problem: "a use of exports"},
		{name: "module", source: react + "\nmodule.exports.useRef = null;\n", problem: "a use of module"},
		{name: "no export of the dispatcher's object", source: strings.Replace(react, "exports.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE =\n  ReactSharedInternals;", "", 1), problem: "no statement exports.__CLIENT_INTERNALS"},
		// React 18 keeps the dispatcher in ReactCurrentDispatcher.current, as react.production.min.js
		// 18.3.1 writes it.
		{name: "React 18", problem: "no statement exports.__CLIENT_INTERNALS", source: `
var U = { current: null }, W = { ReactCurrentDispatcher: U };
exports.__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED = W;
exports.useRef = function (a) { return U.current.useRef(a); };
exports.useState = function (a) { return U.current.useState(a); };
`},
		// A development build asks resolveDispatcher, which warns of a hook called outside a render, as
		// react.development.js of React 19 does.
		{name: "a development build", problem: "no hook exported as a function that only calls ReactSharedInternals.H", source: `
var ReactSharedInternals = { H: null, A: null };
function resolveDispatcher() {
  var dispatcher = ReactSharedInternals.H;
  null === dispatcher && console.error('Invalid hook call.');
  return dispatcher;
}
exports.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = ReactSharedInternals;
exports.useRef = function (initialValue) { return resolveDispatcher().useRef(initialValue); };
`},
	}
	sources := make([]string, len(cases))
	for i, c := range cases {
		sources[i] = c.source
	}
	results := hookWrappersOnNode(t, sources)
	for i, c := range cases {
		got := results[i]
		t.Run(c.name, func(t *testing.T) {
			problems := strings.Join(got.Problems, "; ")
			if c.problem == "" {
				if problems != "" {
					t.Fatalf("problems: %s", problems)
				}
			} else {
				if !strings.Contains(problems, c.problem) {
					t.Fatalf("problems %q, want one with %q", problems, c.problem)
				}
				if len(got.Hooks) > 0 {
					t.Fatalf("hooks %v with problems", got.Hooks)
				}
				return
			}
			if _, ok := got.Hooks["useRef"]; ok == c.noRef {
				t.Errorf("useRef in the hooks: %v, want %v", ok, !c.noRef)
			}
			if c.name == "a wrapper that passes its parameters in another order" {
				if _, ok := got.Hooks["useMemo"]; ok {
					t.Errorf("useMemo counts as a wrapper")
				}
			}
		})
	}
}

// fakeReact is a chunk in the form rolldown writes React in: its file, its index.js, and the
// plugin's plain namespace. The dispatcher logs this, how many arguments it got and their values.
const fakeReact = `
var __commonJSMin = (cb, mod) => () => (mod || cb((mod = { exports: {} }).exports, mod), mod.exports);
var __tifaV = (mod, node) => ({ ...mod });
var require_react_production = /* @__PURE__ */ __commonJSMin(((exports) => {
	var ReactSharedInternals = { H: null };
	exports.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = ReactSharedInternals;
	exports.useRef = function (initialValue) { return ReactSharedInternals.H.useRef(initialValue); };
	exports.useState = function (initialState) { return ReactSharedInternals.H.useState(initialState); };
	exports.useMemo = function (create, deps) { return ReactSharedInternals.H.useMemo(create, deps); };
	exports.useDebugValue = function () {};
}));
var require_react = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = require_react_production();
}));
var trace = [];
var dispatcher = {};
for (const name of ['useRef', 'useState', 'useMemo']) {
	dispatcher[name] = function (...args) { trace.push(name + (this === dispatcher ? ' on H ' : ' on ? ') + args.length + ' ' + args.map(String).join(',')); return name; };
}
require_react_production().__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE.H = dispatcher;
`

// twoReacts is fakeReact with a second copy of React, as a bundle has it when two packages bring
// their own React. Its dispatcher logs that it is the second one.
const twoReacts = fakeReact + `
var require_react_production$1 = /* @__PURE__ */ __commonJSMin(((exports) => {
	var ReactSharedInternals = { H: null };
	exports.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = ReactSharedInternals;
	exports.useRef = function (initialValue) { return ReactSharedInternals.H.useRef(initialValue); };
}));
var require_react$1 = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = require_react_production$1();
}));
var dispatcher2 = { useRef: function (...args) { trace.push('useRef on ' + (this === dispatcher2 ? 'H2 ' : '? ') + args.length + ' ' + args.join(',')); return 'second'; } };
require_react_production$1().__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE.H = dispatcher2;
`

// secondCopy is the second copy of React in twoReacts.
var secondCopy = reactCopy{Hooks: map[string]int{"useRef": 1}, Required: "require_react_production$1"}

// react18 is a chunk with React 18, whose dispatcher is ReactCurrentDispatcher.current, as
// react.production.min.js 18.3.1 has it.
const react18 = `
var __commonJSMin = (cb, mod) => () => (mod || cb((mod = { exports: {} }).exports, mod), mod.exports);
var __tifaV = (mod, node) => ({ ...mod });
var require_react_production_min = /* @__PURE__ */ __commonJSMin(((exports) => {
	var U = { current: null };
	exports.__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED = { ReactCurrentDispatcher: U };
	exports.useRef = function (a) { return U.current.useRef(a); };
}));
var require_react = /* @__PURE__ */ __commonJSMin(((exports, module) => {
	module.exports = require_react_production_min();
}));
var trace = [];
var dispatcher = { useRef: function (...args) { trace.push('useRef on ' + (this === dispatcher ? 'H ' : '? ') + args.join(',')); return 'ref'; } };
require_react_production_min().__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED.ReactCurrentDispatcher.current = dispatcher;
`

// reactCopy is a copy of React in a chunk for callDispatchers: the hooks of its file and its
// require function.
type reactCopy struct {
	Hooks    map[string]int `json:"hooks"`
	Required string         `json:"required"`
}

// fakeCopy is the copy of React that fakeReact has.
var fakeCopy = reactCopy{Hooks: map[string]int{"useRef": 1, "useState": 1, "useMemo": 2}, Required: "require_react_production"}

// dispatcherChunk is a chunk for callDispatchers and the copies of React to give it.
type dispatcherChunk struct {
	Chunk  string      `json:"chunk"`
	Copies []reactCopy `json:"copies"`
}

// dispatcherOnNode runs callDispatchers on each chunk, and runs the function run of the chunk
// before and after.
func dispatcherOnNode(t *testing.T, chunks []dispatcherChunk) []smallResult {
	t.Helper()
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(chunks)
	if err != nil {
		t.Fatal(err)
	}
	script := `
const plugin = await import(process.argv[1])
const { parseAst } = await import('vite')
const run = (code) => {
  try {
    const value = new Function('"use strict";\n' + code + '\nreturn [run(), trace]')()
    return JSON.stringify(value)
  } catch (err) {
    return 'throws ' + err.name + ': ' + err.message
  }
}
const out = JSON.parse(process.argv[2]).map(({ chunk, copies }) => {
  const r = plugin.callDispatchers(chunk, (code) => parseAst(code), copies.map((c) => ({ hooks: new Map(Object.entries(c.hooks)), required: c.required })))
  return { Code: r.code, Count: r.count, Before: run(chunk), After: run(r.code) }
})
process.stdout.write(JSON.stringify(out))
`
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("callDispatchers: %v", err)
	}
	var results []smallResult
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return results
}

// Calls of React's hooks through React's exports go to the dispatcher, with the same this, the
// same arguments and the same value. Calls the plugin cannot prove to be of React's wrappers stay.
func TestPluginCallsTheDispatcherOfReact(t *testing.T) {
	cases := []struct {
		name, source string
		want         int
		prelude      string      // the chunk before source, fakeReact when empty
		copies       []reactCopy // the copies of React, fakeCopy when nil
	}{
		{name: "a plain namespace", want: 1, source: `
var import_react = /* @__PURE__ */ __tifaV(require_react(), 1);
function run() { return import_react.useRef(1); }
`},
		{name: "a call with an empty this and a missing argument", want: 1, source: `
var import_react = __tifaV(require_react());
function run() { return (0, import_react.useState)(); }
`},
		{name: "React's exports and its own file", want: 2, source: `
var React = require_react();
var direct = require_react_production();
function run() { return [React.useMemo(() => 1, [1]), direct.useRef(2)]; }
`},
		{name: "a namespace set when its module first runs", want: 1, source: `
var lazy;
function init() { lazy = __tifaV(require_react(), 1); }
function run() { init(); return lazy.useRef(3); }
`},
		{name: "a name that hides the namespace", source: `
var import_react = __tifaV(require_react(), 1);
function run() { return (function (import_react) { return import_react.useRef(4); })({ useRef: (v) => 'other ' + v }); }
`},
		{name: "a namespace that is set to something else", source: `
var maybe = __tifaV(require_react(), 1);
function run() { maybe = { useRef: (v) => 'other ' + v }; return maybe.useRef(5); }
`},
		{name: "a parameter with the name of React's require function, and of the wrapper", source: `
function other(require_react_production, __tifaV) { var R = require_react_production(), N = __tifaV(require_react_production(), 1); return [R.useRef(11), N.useRef(12)]; }
function run() { var fake = () => ({ useRef: (v) => 'other ' + v }); return other(fake, (m) => ({ useRef: (v) => 'wrapped ' + m.useRef(v) })); }
`},
		{name: "a spread, an optional call and an extra argument", source: `
var import_react = __tifaV(require_react(), 1);
function run() { var a = [6]; return [import_react.useRef(...a), import_react.useRef?.(7), import_react.useRef(8, 9)]; }
`},
		{name: "a namespace read with a computed key", want: 1, source: `
var import_react = __tifaV(require_react(), 1);
var use = import_react[' useRef '.trim()];
function run() { return [use(13), import_react.useRef(14)]; }
`},
		{name: "React's exports set by the chunk", source: `
var React = require_react();
var import_react = __tifaV(require_react(), 1);
function run() { React.useRef = (v) => 'patched ' + v; return [React.useRef(15), import_react.useState(16)]; }
`},
		{name: "React's exports read by a function, as Babel's _interopRequireDefault (react-transition-group) and emotion's _interopNamespace do", want: 1, source: `
var React = require_react();
function _interopNamespace(e) { var n = Object.create(null); Object.keys(e).forEach(function (k) { Object.defineProperty(n, k, { enumerable: true, get: function () { return e[k]; } }); }); n['default'] = e; return Object.freeze(n); }
var React__namespace = _interopNamespace(React);
function run() { return [React.useState(17), React__namespace.useRef(19)]; }
`},
		{name: "React's exports set through Reflect", source: `
var React = require_react();
function run() { Reflect.set(React, 'useState', () => 'patched'); return React.useState(20); }
`},
		{name: "React's exports handed on as they are made", source: `
var import_react = __tifaV(require_react(), 1);
function run() { Object.assign(require_react(), { useMemo: () => 'patched' }); return import_react.useRef(18); }
`},
		{name: "a function that is no wrapper", source: `
var import_react = __tifaV(require_react(), 1);
function run() { return [import_react.useDebugValue(1), import_react['useRef'](10)]; }
`},
		{name: "two copies of React, each with its dispatcher", want: 3, prelude: twoReacts, copies: []reactCopy{fakeCopy, secondCopy}, source: `
var a = __tifaV(require_react(), 1), b = __tifaV(require_react$1(), 1);
function run() { return [a.useRef(21), b.useRef(22), a.useState(23)]; }
`},
		{name: "two copies of React, one of them known", want: 2, prelude: twoReacts, source: `
var a = __tifaV(require_react(), 1), b = __tifaV(require_react$1(), 1);
function run() { return [a.useRef(24), b.useRef(25), a.useState(26)]; }
`},
		// reactHookWrappers finds no hooks in a React of another form, as React 18's.
		{name: "React 18", prelude: react18, copies: []reactCopy{{Hooks: map[string]int{}, Required: "require_react_production_min"}}, source: `
var import_react = __tifaV(require_react(), 1);
function run() { return import_react.useRef(27); }
`},
		{name: "no React the plugin knows", copies: []reactCopy{}, source: `
var import_react = __tifaV(require_react(), 1);
function run() { return import_react.useRef(28); }
`},
	}
	chunks := make([]dispatcherChunk, len(cases))
	for i, c := range cases {
		if c.prelude == "" {
			cases[i].prelude = fakeReact
		}
		if c.copies == nil {
			cases[i].copies = []reactCopy{fakeCopy}
		}
		chunks[i] = dispatcherChunk{Chunk: cases[i].prelude + c.source, Copies: cases[i].copies}
	}
	results := dispatcherOnNode(t, chunks)
	for i, c := range cases {
		got := results[i]
		t.Run(c.name, func(t *testing.T) {
			if got.Count != c.want {
				t.Errorf("%d calls changed, want %d:\n%s", got.Count, c.want, got.Code[len(c.prelude):])
			}
			if got.Before != got.After {
				t.Errorf("gives %s, want %s:\n%s", got.After, got.Before, got.Code[len(c.prelude):])
			}
			if strings.Contains(got.Before, "throws") {
				t.Errorf("the case itself is broken: %s", got.Before)
			}
			if strings.Count(got.Code, "\n") != strings.Count(c.prelude+c.source, "\n") {
				t.Errorf("the lines changed:\n%s", got.Code[len(c.prelude):])
			}
		})
	}
}
