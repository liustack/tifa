package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// callPlugin calls the function name of the tifa Vite plugin on Node with args and decodes what
// it returns into result.
func callPlugin(t *testing.T, name string, args []any, result any) {
	t.Helper()
	testModules(t)
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	script := "const plugin = await import(process.argv[1])\n" +
		"process.stdout.write(JSON.stringify(await plugin." + name + "(...JSON.parse(process.argv[2]))))\n"
	out, err := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input)).Output()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal(out, result); err != nil {
		t.Fatalf("%s: %v: %s", name, err, out)
	}
}

// The check of the SSR bundle skips a bundle that never names require or import() in its code.
// The words in comments, strings and regular expressions do not count: Vite keeps the comments of
// packages in a development bundle, and a check costs more than a rebuild of it.
func TestPluginSeesRequireAndImportOnlyInCode(t *testing.T) {
	for code, want := range map[string]bool{
		"// protocol-relative URLs require a separate check\nx()": false,
		"/* import(name) */ x()":                                  false,
		"x('require'); y(\"require(\")":                           false,
		"x(`require ${a} import(b)`)":                             false,
		"x(/require\\(/.test(s))":                                 false,
		"const requireX = 1, $require = 2, require$ = 3":          false,
		"require('fs')":                                           true,
		"f = () => require(name)":                                 true,
		"import(name)":                                            true,
		"import /* c */ (name)":                                   true,
		"import\n(name)":                                          true,
		"x(`a ${require('fs')} b`)":                               true,
		"x(`a ${{ a: `${1}` }.a} ${require('fs')}`)":              true,
		"x = a / 2; require('y')":                                 true,
		"x = (a) / 2 / b; require('y')":                           true,
		"return /'/.test(s); require('y')":                        true,
		"if (x) y = /`/; require('y')":                            true,
		"x['require'](name)":                                      false,
		"x.require(name)":                                         true,
	} {
		var got bool
		callPlugin(t, "mayLoadModules", []any{code}, &got)
		if got != want {
			t.Errorf("mayLoadModules(%q) = %v, want %v", code, got, want)
		}
	}
}

// guarded is what guardSwitches of the tifa Vite plugin does to a source, run on Node with the
// parser of Vite: the new source, whether it parses, the switches it changed, and, when the source
// declares a function f, what f returns or throws for each of switchInputs, before and after.
type guarded struct {
	Code     string
	Parses   bool
	Switches []struct {
		Name  string
		Cases []string
	}
	Before, After []string
}

// switchInputs is what guardedOnNode passes to f: names that match cases of the sources of
// TestPluginGuardsSwitchesOnStrings and names that do not, other values that are no strings, and
// objects that turn into a name of a case.
const switchInputs = "['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', '', 'A', 'a ', 'é', '🙂', '\\u2028', 'default', " +
	"'constructor', '__proto__', 'toString', 'hasOwnProperty', 0, 1, -0, NaN, null, undefined, true, {}, [], ['a'], " +
	"{ toString: () => 'a' }, new String('a'), Symbol('a'), 1n]"

// guardedOnNode runs guardSwitches on each of sources, a file each when files is true.
func guardedOnNode(t *testing.T, sources []string, files bool) []guarded {
	t.Helper()
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	script := "import fs from 'node:fs'\n" +
		"const plugin = await import(process.argv[1])\n" +
		"const { parseAst } = await import('vite')\n" +
		"const files = process.argv[3] === 'files'\n" +
		"const parses = (code) => {\n" +
		"  try {\n" +
		"    return !!parseAst(code)\n" +
		"  } catch {\n" +
		"    return false\n" +
		"  }\n" +
		"}\n" +
		"// A function body cannot start with a hashbang, which only a script or a module may.\n" +
		"const run = (code) => {\n" +
		"  if (!/function f\\(/.test(code)) return []\n" +
		"  let f\n" +
		"  try {\n" +
		"    f = new Function(code.replace(/^#!.*/, '') + '\\nreturn f')()\n" +
		"  } catch (err) {\n" +
		"    return ['does not compile: ' + err.message]\n" +
		"  }\n" +
		"  return " + switchInputs + ".map((x) => {\n" +
		"    try {\n" +
		"      return JSON.stringify(f(x)) ?? 'undefined'\n" +
		"    } catch (err) {\n" +
		"      return 'throws ' + err.message\n" +
		"    }\n" +
		"  })\n" +
		"}\n" +
		"const results = JSON.parse(process.argv[2]).map((name) => {\n" +
		"  const source = files ? fs.readFileSync(name, 'utf8') : name\n" +
		"  const { code, switches } = plugin.guardSwitches(source, parseAst)\n" +
		"  return { code: files ? '' : code, parses: parses(code), switches, before: files ? [] : run(source), after: files ? [] : run(code) }\n" +
		"})\n" +
		"process.stdout.write(JSON.stringify(results))\n"
	mode := "sources"
	if files {
		mode = "files"
	}
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input), mode)
	cmd.Dir = testdata(t)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("guardSwitches: %v\n%s", err, stderr.String())
	}
	var results []guarded
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("guardSwitches: %v: %s", err, out)
	}
	return results
}

// nineCases is the cases a to i of a switch, each adding its name to the trace t and leaving the
// switch, but for some that fall through, return or throw.
const nineCases = `case "a": t.push("a"); break;
    case "b":
    case "c": t.push("c");
    case "d": t.push("d"); break;
    case "e": return "returns e";
    case "f": throw new Error("throws f");
    case "g": t.push("g"); break;
    case "h": t.push("h"); break;
    case "i": t.push("i"); break;
`

// The tifa plugin puts a Set check in front of a long switch on strings when that does the same,
// and leaves every other switch as it is.
func TestPluginGuardsSwitchesOnStrings(t *testing.T) {
	sw := func(head, cases, tail string) string {
		return "function f(x) {\n  var t = [];\n  " + head + " {\n    " + cases + tail + "  }\n  t.push(\"end\");\n  return t;\n}\n"
	}
	guards := map[string]struct {
		source string
		names  []string
	}{
		"default":    {sw("switch (x)", nineCases, "    default: t.push(\"default\"); if (x === 0) break; t.push(\"after\");\n"), []string{"x"}},
		"no default": {sw("switch (x)", nineCases, ""), []string{"x"}},
		"label": {sw("out: switch (x)", strings.Replace(nineCases, `case "a": t.push("a"); break;`, `case "a": t.push("a"); if (t) break out; t.push("never");`, 1),
			"    default: t.push(\"default\"); break out;\n"), []string{"x"}},
		"loop": {"function f(x) {\n  var t = [];\n  for (var i = 0; i < 3; i++) {\n    switch (x) {\n      " +
			strings.ReplaceAll(nineCases, "break;", "continue;") + "      default: t.push(i); if (i === 1) break; continue;\n    }\n    t.push(\"after \" + i);\n  }\n  return t;\n}\n", []string{"x"}},
		"if without else": {"function f(x) {\n  var t = [];\n  if (typeof x === \"string\") switch (x) {\n    " + nineCases +
			"  } else t.push(\"not a string\");\n  return t;\n}\n", []string{"x"}},
		"if with else": {"function f(x) {\n  var t = [];\n  if (typeof x === \"string\") switch (x) {\n    " + nineCases +
			"    default: t.push(\"default\");\n  } else t.push(\"not a string\");\n  return t;\n}\n", []string{"x"}},
		"var": {sw("switch (x)", strings.Replace(nineCases, `case "a": t.push("a"); break;`, `case "a": var v = "a"; t.push(v); break;`, 1),
			"    default: t.push(v);\n"), []string{"x"}},
		"nested": {sw("switch (x)", strings.Replace(nineCases, `case "g": t.push("g"); break;`,
			"case \"g\": var y = x + \"\";\n      switch (y) {\n        "+nineCases+"        default: t.push(\"inner\");\n      }\n      break;", 1),
			"    default: t.push(\"default\");\n"), []string{"x"}},
		"two": {"function f(x) {\n  return [g(x), h(x)];\n}\n" +
			"function g(x) {\n  var t = [];\n  switch (x) {\n    " + nineCases + "  }\n  return t;\n}\n" +
			"function h(name) {\n  var t = [];\n  switch (name) {\n    " + nineCases + "    default: t.push(\"default\");\n  }\n  return t;\n}\n", []string{"x", "name"}},
		"duplicate": {sw("switch (x)", strings.Replace(nineCases, `case "i":`, `case "a": t.push("second a"); break;
    case "i":`, 1), "    default: t.push(\"default\");\n"), []string{"x"}},
		"not ascii": {"// é 🙂\nvar s = \"\\u2028 é\";\n" + sw("switch (x)", strings.NewReplacer(`"a"`, `"é"`, `"b"`, `"🙂"`, `"c"`, `" "`, `"d"`, `""`).Replace(nineCases),
			"    default: t.push(\"default\");\n"), []string{"x"}},
		"closure":   {"function f(x) {\n  var y = x;\n  return (() => {\n    var t = [];\n    switch (y) {\n      " + nineCases + "      default: t.push(\"default\");\n    }\n    return t;\n  })();\n}\n", []string{"y"}},
		"hashbang":  {"#!/usr/bin/env node\n" + sw("switch (x)", nineCases, "    default: t.push(\"default\");\n"), []string{"x"}},
		"directive": {"'use strict';\n\"use something\";\n// a comment\n" + sw("switch (x)", nineCases, "    default: t.push(this === undefined ? \"strict\" : \"sloppy\");\n"), []string{"x"}},
	}
	keeps := map[string]string{
		"default in the middle":     sw("switch (x)", strings.Replace(nineCases, `case "i":`, "default: t.push(\"default\"); break;\n    case \"i\":", 1), ""),
		"falls into default":        sw("switch (x)", strings.Replace(nineCases, `case "i": t.push("i"); break;`, `case "i": t.push("i");`, 1), "    default: t.push(\"default\");\n"),
		"empty case before default": sw("switch (x)", nineCases+"    case \"j\":\n", "    default: t.push(\"default\");\n"),
		"block before default":      sw("switch (x)", strings.Replace(nineCases, `case "i": t.push("i"); break;`, `case "i": { t.push("i"); break; }`, 1), "    default: t.push(\"default\");\n"),
		"let":                       sw("switch (x)", strings.Replace(nineCases, `case "a": t.push("a"); break;`, `case "a": let v = "a"; t.push(v); break;`, 1), ""),
		"const":                     sw("switch (x)", strings.Replace(nineCases, `case "a": t.push("a"); break;`, `case "a": const v = "a"; t.push(v); break;`, 1), ""),
		"class":                     sw("switch (x)", strings.Replace(nineCases, `case "a": t.push("a"); break;`, `case "a": class C {} t.push(C.name); break;`, 1), ""),
		"function":                  sw("switch (x)", strings.Replace(nineCases, `case "a": t.push("a"); break;`, `case "a": function g() {} t.push(g.name); break;`, 1), ""),
		"member":                    sw("var o = { x: x };\n  switch (o.x)", nineCases, ""),
		"call":                      sw("switch (String(x))", nineCases, ""),
		"number case":               sw("switch (x)", nineCases+"    case 1: t.push(1); break;\n", ""),
		"template case":             sw("switch (x)", nineCases+"    case `j`: t.push(\"j\"); break;\n", ""),
		"identifier case":           sw("var j = \"j\";\n  switch (x)", nineCases+"    case j: t.push(\"j\"); break;\n", ""),
		"seven cases":               sw("switch (x)", strings.Replace(strings.Replace(nineCases, `case "h": t.push("h"); break;`, "", 1), `case "i": t.push("i"); break;`, "", 1), "    default: t.push(\"default\");\n"),
		"global beside a let": "var calls = 0;\n" + sw("Object.defineProperty(globalThis, \"gx\", { get() { calls++; return x; }, configurable: true });\n"+
			"  { let gx = 1; t.push(gx); }\n  switch (gx)", nineCases, "    default: t.push(calls);\n") + "",
		"global beside a const": "var calls = 0;\n" + sw("Object.defineProperty(globalThis, \"gx\", { get() { calls++; return x; }, configurable: true });\n"+
			"  if (calls >= 0) { const gx = 1; t.push(gx); }\n  switch (gx)", nineCases, "    default: t.push(calls);\n"),
		"global beside a block function": "\"use strict\";\nvar calls = 0;\n" + sw("Object.defineProperty(globalThis, \"gx\", { get() { calls++; return x; }, configurable: true });\n"+
			"  { function gx() {} t.push(typeof gx); }\n  switch (gx)", nineCases, "    default: t.push(calls);\n"),
		"global": "var calls = 0;\n" + sw("Object.defineProperty(globalThis, \"name\", { get() { calls++; return x; }, configurable: true });\n  t.push(calls);\n  switch (name)",
			nineCases, "    default: t.push(calls);\n"),
		"named like its Sets": "var tifaSwitchCases = 1;\n" + sw("switch (x)", nineCases, ""),
	}
	var names, sources []string
	for name, g := range guards {
		names, sources = append(names, name), append(sources, g.source)
	}
	for name, source := range keeps {
		names, sources = append(names, name), append(sources, source)
	}
	results := guardedOnNode(t, sources, false)
	for i, name := range names {
		got := results[i]
		if !got.Parses {
			t.Errorf("%s: the new source does not parse:\n%s", name, got.Code)
		}
		if strings.Count(got.Code, "\n") != strings.Count(sources[i], "\n") {
			t.Errorf("%s: the lines of the source moved:\n%s", name, got.Code)
		}
		if len(got.Before) != len(got.After) || len(got.Before) == 0 {
			t.Errorf("%s: f ran %d times before and %d after: %q", name, len(got.Before), len(got.After), got.After)
			continue
		}
		for k := range got.Before {
			if got.Before[k] != got.After[k] {
				t.Errorf("%s: input %d: f returns %s, want %s\n%s", name, k, got.After[k], got.Before[k], got.Code)
			}
		}
		g, guard := guards[name]
		if !guard {
			if got.Code != sources[i] || len(got.Switches) != 0 {
				t.Errorf("%s: the source changed to\n%s", name, got.Code)
			}
			continue
		}
		var switched []string
		for _, s := range got.Switches {
			switched = append(switched, s.Name)
			if len(s.Cases) < 9 {
				t.Errorf("%s: switch on %s has the cases %q", name, s.Name, s.Cases)
			}
		}
		if !slices.Equal(switched, g.names) {
			t.Errorf("%s: changed the switches on %q, want %q\n%s", name, switched, g.names, got.Code)
		}
		if strings.Count(got.Code, "tifaSwitchCases") != 2*len(g.names) {
			t.Errorf("%s: the source changed to\n%s", name, got.Code)
		}
	}
	if !slices.ContainsFunc(results, func(r guarded) bool { return slices.Contains(r.Before, `["a","end"]`) }) {
		t.Error("no f returned the trace of case a")
	}
}

// rolldownRuntime is how the SSR bundle starts at renderChunk: the IIFE and rolldown's helpers that
// give an ES module the exports of a CommonJS module.
const rolldownRuntime = "(function() {\n\n//#region \\0rolldown/runtime.js\n" +
	"\tvar __create = Object.create;\n" +
	"\tvar __defProp = Object.defineProperty;\n" +
	"\tvar __getOwnPropDesc = Object.getOwnPropertyDescriptor;\n" +
	"\tvar __getOwnPropNames = Object.getOwnPropertyNames;\n" +
	"\tvar __getProtoOf = Object.getPrototypeOf;\n" +
	"\tvar __hasOwnProp = Object.prototype.hasOwnProperty;\n" +
	"\tvar __commonJSMin = (cb, mod) => () => (mod || (cb((mod = { exports: {} }).exports, mod, mod.exports), cb = null), mod.exports);\n" +
	"\tvar __copyProps = (to, from, except, desc) => {\n" +
	"\t\tif (from && typeof from === \"object\" || typeof from === \"function\") {\n" +
	"\t\t\tfor (var keys = __getOwnPropNames(from), i = 0, n = keys.length, key; i < n; i++) {\n" +
	"\t\t\t\tkey = keys[i];\n" +
	"\t\t\t\tif (!__hasOwnProp.call(to, key) && key !== except) {\n" +
	"\t\t\t\t\t__defProp(to, key, {\n" +
	"\t\t\t\t\t\tget: ((k) => from[k]).bind(null, key),\n" +
	"\t\t\t\t\t\tenumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable\n" +
	"\t\t\t\t\t});\n" +
	"\t\t\t\t}\n" +
	"\t\t\t}\n" +
	"\t\t}\n" +
	"\t\treturn to;\n" +
	"\t};\n" +
	"\tvar __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(isNodeMode || !mod || !mod.__esModule || !__hasOwnProp.call(mod, \"default\") ? __defProp(target, \"default\", {\n" +
	"\t\tvalue: mod,\n" +
	"\t\tenumerable: true\n" +
	"\t}) : target, mod));\n\n" +
	"//#endregion\n"

// cjsExports is what CommonJS modules export in namespaceChunk: objects, functions and classes
// with properties, ES modules compiled to CommonJS with and without a default export, and values
// that are no objects.
var cjsExports = []string{
	"{ a: 1, f() { return this }, nested: { b: 2 } }",
	"Object.assign(function f() {}, { a: 1, default: 2 })",
	"class C { static s = 1 }",
	"Object.defineProperties({ a: 1 }, { hidden: { value: 2, enumerable: false }, got: { get() { return 3 }, enumerable: true } })",
	"Object.defineProperty({ default: 'd', a: 1 }, '__esModule', { value: true })",
	"Object.defineProperty({ a: 1 }, '__esModule', { value: true })",
	"Object.create({ inherited: 1 }, { own: { value: 2, enumerable: true } })",
	"null",
	"'text'",
	"42",
}

// namespaceChunk is a chunk at renderChunk with a CommonJS module of pkg for each of cjsExports,
// imported by an ES module with and without Node mode, and at the end the namespaces and the
// exports in globalThis.
func namespaceChunk(pkg string) string {
	var b strings.Builder
	b.WriteString(rolldownRuntime)
	for i, e := range cjsExports {
		fmt.Fprintf(&b, "//#region ../node_modules/.pnpm/%[1]s@1.0.0/node_modules/%[1]s/m%[2]d.js\n"+
			"\tvar require_m%[2]d = /* @__PURE__ */ __commonJSMin(((exports, module) => {\n\t\tmodule.exports = %[3]s;\n\t}));\n\n//#endregion\n", pkg, i, e)
	}
	b.WriteString("//#region app/page.tsx\n")
	for i := range cjsExports {
		fmt.Fprintf(&b, "var ns%[1]d = /* @__PURE__ */ __toESM(require_m%[1]d());\nvar node%[1]d = /* @__PURE__ */ __toESM(require_m%[1]d(), 1);\n", i)
	}
	b.WriteString("globalThis.namespaces = [")
	for i := range cjsExports {
		fmt.Fprintf(&b, "[ns%[1]d, node%[1]d, require_m%[1]d()], ", i)
	}
	b.WriteString("];\n\n//#endregion\n})();\n")
	return b.String()
}

// plain is what plainNamespaces of the tifa Vite plugin does to a chunk, run on Node: the new code,
// the warning, and for each module of the chunk when it runs, its namespaces with and without Node
// mode as the original code and the new one give them.
type plain struct {
	Code, Warning string
	Before, After []string
}

// plainOnNode runs plainNamespaces of the tifa Vite plugin on code, a chunk with the modules ids.
func plainOnNode(t *testing.T, code string, ids []string) plain {
	t.Helper()
	testModules(t)
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal([]any{code, ids})
	if err != nil {
		t.Fatal(err)
	}
	// describe gives each own property of a namespace with its value, as the exports of the module
	// or its own, and whether it is enumerable, and the prototype of the namespace.
	script := "const plugin = await import(process.argv[1])\n" +
		"const [code, ids] = JSON.parse(process.argv[2])\n" +
		"const describe = (ns, exports) => JSON.stringify([Object.getPrototypeOf(ns) === (exports == null ? Object.prototype : Object.getPrototypeOf(Object(exports))),\n" +
		"  Object.getOwnPropertyNames(ns).map((k) => [k, ns[k] === exports ? 'exports' : typeof ns[k] === 'function' ? 'function' : ns[k], Object.getOwnPropertyDescriptor(ns, k).enumerable])])\n" +
		"const run = (code) => {\n" +
		"  try {\n" +
		"    new Function(code)()\n" +
		"  } catch (err) {\n" +
		"    return ['throws ' + err.message]\n" +
		"  }\n" +
		"  return globalThis.namespaces.map(([ns, node, exports]) => describe(ns, exports) + ' ' + describe(node, exports))\n" +
		"}\n" +
		"const { code: changed, warning = '' } = plugin.plainNamespaces(code, ids)\n" +
		"process.stdout.write(JSON.stringify({ code: changed, warning, before: run(code), after: run(changed) }))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("plainNamespaces: %v\n%s", err, stderr.String())
	}
	var result plain
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("plainNamespaces: %v: %s", err, out)
	}
	return result
}

// The tifa plugin gives the ES modules that import a CommonJS module of React or React DOM a
// namespace of plain values in place of rolldown's getters, the same values with the same
// prototype, names and enumerability, and leaves the namespaces of other packages as they are.
func TestPluginGivesReactNamespacesPlainValues(t *testing.T) {
	ids := []string{"/app/node_modules/react/index.js", "/app/node_modules/react-dom/index.js", "/app/page.tsx"}
	// rolldown adds $ and a number to the name of a helper when the bundle has another name like it.
	renamed := strings.NewReplacer("__defProp", "__defProp$34", "__toESM", "__toESM$1", "__commonJSMin", "__commonJSMin$2")
	for _, pkg := range []string{"react", "react-dom", "renamed"} {
		chunk, plainCall := namespaceChunk(pkg), "__tifaV(require_m"
		if pkg == "renamed" {
			chunk, plainCall = renamed.Replace(namespaceChunk("react")), "__tifaV$1(require_m"
		}
		got := plainOnNode(t, chunk, ids)
		if got.Warning != "" {
			t.Errorf("%s: warning %s", pkg, got.Warning)
		}
		if n := strings.Count(got.Code, plainCall); n != 2*len(cjsExports) {
			t.Errorf("%s: %d imports get plain values, want %d:\n%s", pkg, n, 2*len(cjsExports), got.Code)
		}
		// The new code has the lines and the places of the old one, so the source map still fits.
		old, changed := strings.Split(chunk, "\n"), strings.Split(got.Code, "\n")
		if len(changed) != len(old) {
			t.Errorf("%s: %d lines, want %d", pkg, len(changed), len(old))
		} else {
			for i := range old {
				if len(changed[i]) != len(old[i]) && !strings.HasPrefix(changed[i], old[i]) {
					t.Errorf("%s: line %d moved:\n%s\nwas:\n%s", pkg, i+1, changed[i], old[i])
				}
			}
		}
		if !slices.Equal(got.Before, got.After) || len(got.Before) != len(cjsExports) {
			t.Errorf("%s: the namespaces are\n%s\nwant\n%s", pkg, strings.Join(got.After, "\n"), strings.Join(got.Before, "\n"))
		}
	}
	if got := plainOnNode(t, namespaceChunk("abort-controller"), ids); got.Code != namespaceChunk("abort-controller") || got.Warning != "" {
		t.Errorf("the chunk of another package changed to\n%s\nwarning %q", got.Code, got.Warning)
	}
	// Without React in the bundle there is nothing to change and nothing to warn of.
	odd := strings.Replace(namespaceChunk("react"), "get: ((k) => from[k]).bind(null, key)", "get: () => from[key]", 1)
	if got := plainOnNode(t, odd, ids[2:]); got.Code != odd || got.Warning != "" {
		t.Errorf("a bundle without React changed to\n%s\nwarning %q", got.Code, got.Warning)
	}
}

// When rolldown writes the namespaces of CommonJS modules in a form the tifa plugin does not know,
// the build warns, with the packages and why, and the chunk stays as it is.
func TestPluginWarnsWhenItCannotGiveReactNamespacesPlainValues(t *testing.T) {
	chunk := namespaceChunk("react")
	for name, c := range map[string]struct{ code, why string }{
		"getter":     {strings.Replace(chunk, "get: ((k) => from[k]).bind(null, key)", "get: () => from[key]", 1), "writes __toESM"},
		"no __toESM": {strings.Replace(chunk, "\tvar __toESM = (mod, isNodeMode, target) =>", "\tvar __toESM2 = (mod, isNodeMode, target) =>", 1), "writes __toESM"},
		"call":       {strings.Replace(chunk, "var ns0 = /* @__PURE__ */ __toESM(require_m0());", "var ns0 = /* @__PURE__ */ __toESM(globalThis.x ? require_m0() : null);", 1), "calls __toESM"},
		"region":     {strings.Replace(chunk, "//#region ../node_modules/.pnpm/react@1.0.0/node_modules/react/m0.js\n", "", 1), "require_m0"},
		"name":       {strings.Replace(chunk, "})();\n", "var __tifaV = 1;\n})();\n", 1), "__tifaV"},
	} {
		got := plainOnNode(t, c.code, []string{"/app/node_modules/react/index.js", "/app/node_modules/.pnpm/react-dom@1.0.0/node_modules/react-dom/index.js"})
		if got.Code != c.code {
			t.Errorf("%s: the chunk changed to\n%s", name, got.Code)
		}
		if !strings.HasPrefix(got.Warning, "react, react-dom: ") || !strings.Contains(got.Warning, c.why) || !strings.Contains(got.Warning, "plain values") {
			t.Errorf("%s: warning %q, want one with the packages and %q", name, got.Warning, c.why)
		}
	}
}
