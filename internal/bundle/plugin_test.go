package bundle

import (
	"encoding/json"
	"fmt"
	"maps"
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

// rolldownRuntime is how the SSR bundle, an ES module, starts at renderChunk: rolldown's helpers that
// give an ES module the exports of a CommonJS module.
const rolldownRuntime = "//#region \\0rolldown/runtime.js\n" +
	"var __create = Object.create;\n" +
	"var __defProp = Object.defineProperty;\n" +
	"var __getOwnPropDesc = Object.getOwnPropertyDescriptor;\n" +
	"var __getOwnPropNames = Object.getOwnPropertyNames;\n" +
	"var __getProtoOf = Object.getPrototypeOf;\n" +
	"var __hasOwnProp = Object.prototype.hasOwnProperty;\n" +
	"var __commonJSMin = (cb, mod) => () => (mod || (cb((mod = { exports: {} }).exports, mod, mod.exports), cb = null), mod.exports);\n" +
	"var __copyProps = (to, from, except, desc) => {\n" +
	"\tif (from && typeof from === \"object\" || typeof from === \"function\") {\n" +
	"\t\tfor (var keys = __getOwnPropNames(from), i = 0, n = keys.length, key; i < n; i++) {\n" +
	"\t\t\tkey = keys[i];\n" +
	"\t\t\tif (!__hasOwnProp.call(to, key) && key !== except) {\n" +
	"\t\t\t\t__defProp(to, key, {\n" +
	"\t\t\t\t\tget: ((k) => from[k]).bind(null, key),\n" +
	"\t\t\t\t\tenumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable\n" +
	"\t\t\t\t});\n" +
	"\t\t\t}\n" +
	"\t\t}\n" +
	"\t}\n" +
	"\treturn to;\n" +
	"};\n" +
	"var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(isNodeMode || !mod || !mod.__esModule || !__hasOwnProp.call(mod, \"default\") ? __defProp(target, \"default\", {\n" +
	"\tvalue: mod,\n" +
	"\tenumerable: true\n" +
	"}) : target, mod));\n\n" +
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

// chunkPart is a module of a chunk at renderChunk: its id and the code rolldown rendered for it,
// which is in chunk.modules. The chunk is the code of its modules one after the other.
type chunkPart struct{ id, code string }

// namespaceParts is a chunk with a CommonJS module of pkg for each of cjsExports, imported by an
// ES module with and without Node mode, and at the end the namespaces and the exports in
// globalThis.
func namespaceParts(pkg string) []chunkPart {
	parts := []chunkPart{{"\x00rolldown/runtime.js", rolldownRuntime}}
	for i, e := range cjsExports {
		parts = append(parts, chunkPart{
			fmt.Sprintf("/app/node_modules/.pnpm/%[1]s@1.0.0/node_modules/%[1]s/m%[2]d.js", pkg, i),
			fmt.Sprintf("//#region ../node_modules/.pnpm/%[1]s@1.0.0/node_modules/%[1]s/m%[2]d.js\n"+
				"var require_m%[2]d = /* @__PURE__ */ __commonJSMin(((exports, module) => {\n\tmodule.exports = %[3]s;\n}));\n\n//#endregion\n", pkg, i, e),
		})
	}
	var b strings.Builder
	b.WriteString("//#region app/page.tsx\n")
	for i := range cjsExports {
		fmt.Fprintf(&b, "var ns%[1]d = /* @__PURE__ */ __toESM(require_m%[1]d());\nvar node%[1]d = /* @__PURE__ */ __toESM(require_m%[1]d(), 1);\n", i)
	}
	b.WriteString("globalThis.namespaces = [")
	for i := range cjsExports {
		fmt.Fprintf(&b, "[ns%[1]d, node%[1]d, require_m%[1]d()], ", i)
	}
	b.WriteString("];\n\n//#endregion\n")
	return append(parts, chunkPart{"/app/app/page.tsx", b.String()})
}

// edited gives each part of parts the code change gives it.
func edited(parts []chunkPart, change func(string) string) []chunkPart {
	out := make([]chunkPart, len(parts))
	for i, p := range parts {
		out[i] = chunkPart{p.id, change(p.code)}
	}
	return out
}

// chunkOf returns the code of the chunk of parts and its chunk.modules.
func chunkOf(parts []chunkPart) (string, map[string]any) {
	var b strings.Builder
	modules := map[string]any{}
	for _, p := range parts {
		b.WriteString(p.code)
		modules[p.id] = map[string]any{"code": p.code}
	}
	return b.String(), modules
}

// namespaceChunk is the code of namespaceParts(pkg).
func namespaceChunk(pkg string) string {
	code, _ := chunkOf(namespaceParts(pkg))
	return code
}

// plain is what plainNamespaces of the tifa Vite plugin does to a chunk, run on Node: the new code,
// the warning, and for each module of the chunk when it runs, its namespaces with and without Node
// mode as the original code and the new one give them.
type plain struct {
	Code, Warning string
	Before, After []string
}

// plainOnNode runs plainNamespaces of the tifa Vite plugin on code, a chunk with its chunk.modules.
func plainOnNode(t *testing.T, code string, modules map[string]any) plain {
	t.Helper()
	testModules(t)
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal([]any{code, modules})
	if err != nil {
		t.Fatal(err)
	}
	// describe gives each own property of a namespace with its value, as the exports of the module
	// or its own, and whether it is enumerable, and the prototype of the namespace.
	script := "const plugin = await import(process.argv[1])\n" +
		"const [code, modules] = JSON.parse(process.argv[2])\n" +
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
		"const { code: changed, warning = '' } = plugin.plainNamespaces(code, modules)\n" +
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
	// rolldown adds $ and a number to the name of a helper when the bundle has another name like it.
	renamed := strings.NewReplacer("__defProp", "__defProp$34", "__toESM", "__toESM$1", "__commonJSMin", "__commonJSMin$2")
	// A build without minify keeps the comment at the top of each file of React before the require
	// function of the file, as tifa dev builds the SSR bundle: a block on lines of its own, a block
	// on the line of the require function, line comments. TestPluginGivesPlainValuesInWhatRolldownWrites
	// has the real ones.
	forms := map[string]*strings.Replacer{
		"block":      strings.NewReplacer(".js\nvar require_m", ".js\n/**\n* @license React\n* m.js\n*/\nvar require_m"),
		"same line":  strings.NewReplacer(".js\nvar require_m", ".js\n/* @license React */ var require_m"),
		"line":       strings.NewReplacer(".js\nvar require_m", ".js\n// @license React\nvar require_m"),
		"line block": strings.NewReplacer(".js\nvar require_m", ".js\n// @license React\n/* @license React */\n/* more */ var require_m"),
	}
	cases := map[string][]chunkPart{
		"react":     namespaceParts("react"),
		"react-dom": namespaceParts("react-dom"),
		"renamed":   edited(namespaceParts("react"), renamed.Replace),
	}
	for name, form := range forms {
		cases[name] = edited(namespaceParts("react"), form.Replace)
	}
	// From 1.2.15 rolldown starts each module with "// path" and no longer ends it.
	comments := strings.NewReplacer("//#region ", "// ", "//#endregion\n", "")
	for name, parts := range maps.Clone(cases) {
		cases[name+", 1.2.15"] = edited(parts, comments.Replace)
	}
	for name, parts := range cases {
		plainCall := "__tifaV(require_m"
		if strings.HasPrefix(name, "renamed") {
			plainCall = "__tifaV$1(require_m"
		}
		pkg := name
		chunk, modules := chunkOf(parts)
		got := plainOnNode(t, chunk, modules)
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
	// A module of React with no require function of its own: React is in the bundle.
	chunk, modules := chunkOf(append(namespaceParts("abort-controller"), chunkPart{"/app/node_modules/react/index.js", ""}))
	if got := plainOnNode(t, chunk, modules); got.Code != chunk || got.Warning != "" {
		t.Errorf("the chunk of another package changed to\n%s\nwarning %q", got.Code, got.Warning)
	}
	// Without React in the bundle there is nothing to change and nothing to warn of.
	odd := strings.Replace(namespaceChunk("react"), "get: ((k) => from[k]).bind(null, key)", "get: () => from[key]", 1)
	if got := plainOnNode(t, odd, map[string]any{"/app/app/page.tsx": map[string]any{"code": ""}}); got.Code != odd || got.Warning != "" {
		t.Errorf("a bundle without React changed to\n%s\nwarning %q", got.Code, got.Warning)
	}
}

// When rolldown writes the namespaces of CommonJS modules in a form the tifa plugin does not know,
// or no module of the chunk declares a require function that __toESM gets, the build warns, with
// the packages and why, and the chunk stays as it is.
func TestPluginWarnsWhenItCannotGiveReactNamespacesPlainValues(t *testing.T) {
	react := namespaceParts("react")
	m0 := react[1].id
	region := "//#region ../node_modules/.pnpm/react@1.0.0/node_modules/react/m0.js\n"
	replace := func(old, new string) func(string) string {
		return func(code string) string { return strings.Replace(code, old, new, 1) }
	}
	without := func(parts []chunkPart, id string) map[string]any {
		_, modules := chunkOf(parts)
		delete(modules, id)
		return modules
	}
	for name, c := range map[string]struct {
		parts   []chunkPart
		modules map[string]any // chunk.modules when not that of parts
		why     string
	}{
		"getter":     {edited(react, replace("get: ((k) => from[k]).bind(null, key)", "get: () => from[key]")), nil, "writes __toESM"},
		"no __toESM": {edited(react, replace("var __toESM = (mod, isNodeMode, target) =>", "var __toESM2 = (mod, isNodeMode, target) =>")), nil, "writes __toESM"},
		"call":       {edited(react, replace("var ns0 = /* @__PURE__ */ __toESM(require_m0());", "var ns0 = /* @__PURE__ */ __toESM(globalThis.x ? require_m0() : null);")), nil, "calls __toESM"},
		// The module starts with code before its require function.
		"code": {edited(react, replace(region, region+"globalThis.x = 1;\n")), nil, "require_m0"},
		// A comment that is never closed.
		"open comment": {edited(react, replace(region, region+"/* never closed\n")), nil, "require_m0"},
		// No module of the chunk has the code that declares require_m0.
		"no module": {react, without(react, m0), "require_m0"},
		"null code": {react, func() map[string]any {
			modules := without(react, m0)
			modules[m0] = map[string]any{"code": nil}
			return modules
		}(), "require_m0"},
		// Two modules of two packages declare require_m0.
		"twice": {append(slices.Clone(react), chunkPart{"/app/node_modules/other/m0.js", react[1].code}), nil, "require_m0"},
		"name":  {append(slices.Clone(react), chunkPart{"/app/app/extra.js", "var __tifaV = 1;\n"}), nil, "__tifaV"},
	} {
		code, modules := chunkOf(c.parts)
		if c.modules != nil {
			modules = c.modules
		}
		modules["/app/node_modules/.pnpm/react-dom@1.0.0/node_modules/react-dom/index.js"] = map[string]any{"code": ""}
		got := plainOnNode(t, code, modules)
		if got.Code != code {
			t.Errorf("%s: the chunk changed to\n%s", name, got.Code)
		}
		if !strings.HasPrefix(got.Warning, "react, react-dom: ") || !strings.Contains(got.Warning, c.why) || !strings.Contains(got.Warning, "plain values") {
			t.Errorf("%s: warning %q, want one with the packages and %q", name, got.Warning, c.why)
		}
	}
}

// The CommonJS files of React keep the comments at their top in a build without minify, which
// rolldown writes before the require function of the file in several forms. plainNamespaces reads
// past each of them in what rolldown writes, with and without minify, with the line of each module
// as rolldown 1.2.14 writes it and as 1.2.15 does. An ES module before them ends in a line comment,
// and the comments of a module of another package hold the line of a module of React and the start
// of a comment, which leave its namespace as it is.
func TestPluginGivesPlainValuesInWhatRolldownWrites(t *testing.T) {
	modules := testModules(t)
	root := t.TempDir()
	files := map[string]string{
		// React's own form: a block, an empty line and "use strict".
		"node_modules/react/block.js":     "/**\n * @license React\n * block.js\n */\n\n'use strict';\nexports.a = 1;\n",
		"node_modules/react/sameline.js":  "/* @license React */ 'use strict'; exports.b = 2;\n",
		"node_modules/react/line.js":      "// @license React\n'use strict';\nexports.c = 3;\n",
		"node_modules/react/two.js":       "// @license React\n/** @license React */\n'use strict';\nexports.d = 4;\n",
		"node_modules/react/package.json": `{"name":"react"}`,
		"node_modules/react/index.js":     "'use strict';\nexports.f = 6;\n",
		"node_modules/other/index.js":     "/* @license other */ 'use strict'; exports.e = 5;\n",
		"node_modules/other/package.json": `{"name":"other"}`,
		// Comments of another package that hold the line of a module of React and the start of a
		// comment. Its exports change, which a namespace of plain values would not show.
		"node_modules/tricky/index.js": "/* @license example\n// node_modules/react/index.js\n/* example of a comment opener\n*/\n" +
			"'use strict';\nexports.value = 1;\nexports.change = () => { exports.value = 2 };\n",
		"node_modules/tricky/package.json": `{"name":"tricky"}`,
		"tail.js":                          "export const tail = globalThis.x;\n// @license the end of tail.js\n",
		"entry.js": "import { tail } from './tail.js'\nimport * as block from 'react/block.js'\nimport * as sameline from 'react/sameline.js'\n" +
			"import * as line from 'react/line.js'\nimport * as two from 'react/two.js'\nimport * as index from 'react'\nimport * as other from 'other'\n" +
			"import * as tricky from 'tricky'\n" +
			"globalThis.out = [tail, block, sameline, line, two, index, other, tricky]\n",
	}
	writeTree(t, root, files)
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	// The comments Vite 8 keeps: { annotation, jsdoc, legal } all without minify, none with it.
	script := "const fs = await import('node:fs')\nconst { createRequire } = await import('node:module')\n" +
		"const [plugin, vite, dir] = process.argv.slice(1)\n" +
		"const root = fs.realpathSync(dir)\n" +
		"const { rolldown } = await import(createRequire(fs.realpathSync(vite)).resolve('rolldown'))\n" +
		"const { plainNamespaces } = await import(plugin)\n" +
		"const out = []\n" +
		"for (const minify of [false, true]) {\n" +
		"  let code, modules\n" +
		"  const bundle = await rolldown({ cwd: root, input: root + '/entry.js', plugins: [{ name: 'see', renderChunk(c, chunk) { code = c; modules = chunk.modules; return null } }] })\n" +
		"  await bundle.generate({ format: 'esm', minify, comments: { annotation: !minify, jsdoc: !minify, legal: !minify } })\n" +
		"  for (const from1215 of [false, true]) {\n" +
		"    const form = (code) => from1215 ? code.replaceAll('//#region ', '// ').replaceAll('//#endregion\\n', '') : code\n" +
		"    const chunk = form(code)\n" +
		"    const rendered = Object.fromEntries(Object.entries(modules).map(([id, m]) => [id, { code: m.code === null ? null : form(m.code) }]))\n" +
		"    const { code: changed, warning = '' } = plainNamespaces(chunk, rendered)\n" +
		"    new Function(changed)()\n" +
		"    const tricky = globalThis.out[7]\n" +
		"    tricky.change()\n" +
		"    out.push({ minify, from1215, code: chunk, changed, warning, tricky: tricky.value })\n" +
		"  }\n" +
		"}\n" +
		"process.stdout.write(JSON.stringify(out))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, filepath.Join(modules, "vite", "package.json"), root)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	var results []struct {
		Minify, From1215       bool
		Code, Changed, Warning string
		Tricky                 int
	}
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, r := range results {
		name := fmt.Sprintf("minify %v, 1.2.15 %v", r.Minify, r.From1215)
		if !r.Minify {
			// The forms this test is for, as rolldown writes them.
			for _, form := range []string{"*/\nvar require_block", "/* @license React */ var require_sameline", "// @license React\nvar require_line", "*/\nvar require_two", "// @license the end of tail.js\n"} {
				if !strings.Contains(r.Code, form) {
					t.Errorf("%s: rolldown no longer writes %q:\n%s", name, form, r.Code)
				}
			}
		}
		if r.Warning != "" {
			t.Errorf("%s: warning %s\n%s", name, r.Warning, r.Code)
		}
		for _, module := range []string{"block", "sameline", "line", "two", "react"} {
			if !strings.Contains(r.Changed, "__tifaV(require_"+module+"()") {
				t.Errorf("%s: the module %s of react gets no plain values:\n%s", name, module, r.Changed)
			}
		}
		for _, module := range []string{"other", "tricky"} {
			if !strings.Contains(r.Changed, "__toESM(require_"+module+"()") {
				t.Errorf("%s: the module of %s changed:\n%s", name, module, r.Changed)
			}
		}
		if r.Tricky != 2 {
			t.Errorf("%s: the namespace of tricky reads value %d after it changed to 2", name, r.Tricky)
		}
	}
}

// plainNamespaces reads each module once and writes the chunk once, so a chunk of 10,000 CommonJS
// modules, half of them React's, takes it far less than a second. tifa dev runs it on every
// rebuild of the SSR bundle.
func TestPlainNamespacesTakesLinearTime(t *testing.T) {
	testModules(t)
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "const { plainNamespaces } = await import(process.argv[1])\n" +
		"const runtime = JSON.parse(process.argv[2])\n" +
		"const time = (n) => {\n" +
		"  const modules = {}, parts = [runtime], calls = []\n" +
		"  for (let i = 0; i < n; i++) {\n" +
		"    const pkg = i % 2 ? 'react' : 'other'\n" +
		"    const code = `//#region ../node_modules/${pkg}/m${i}.js\\n/** @license ${pkg} */\\nvar require_m${i} = /* @__PURE__ */ __commonJSMin(((exports) => {\\n\\texports.a = ${i};\\n}));\\n\\n//#endregion\\n`\n" +
		"    modules[`/app/node_modules/${pkg}/m${i}.js`] = { code }\n" +
		"    parts.push(code)\n" +
		"    calls.push(`var ns${i} = /* @__PURE__ */ __toESM(require_m${i}());\\n`)\n" +
		"  }\n" +
		"  const code = parts.join('') + '//#region app/page.tsx\\n' + calls.join('') + '//#endregion\\n'\n" +
		"  modules['/app/app/page.tsx'] = { code: '' }\n" +
		"  let best = Infinity, result\n" +
		"  for (let round = 0; round < 3; round++) {\n" +
		"    const start = performance.now()\n" +
		"    result = plainNamespaces(code, modules)\n" +
		"    best = Math.min(best, performance.now() - start)\n" +
		"  }\n" +
		"  return { n, ms: best, warning: result.warning ?? '', plain: (result.code.match(/__tifaV\\(require_m/g) ?? []).length }\n" +
		"}\n" +
		"process.stdout.write(JSON.stringify([time(1000), time(10000)]))\n"
	runtime, err := json.Marshal(rolldownRuntime)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", "--input-type=module", "-e", script, plugin, string(runtime)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var runs []struct {
		N       int
		Ms      float64
		Warning string
		Plain   int
	}
	if err := json.Unmarshal(out, &runs); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, r := range runs {
		t.Logf("%d modules: %.1f ms", r.N, r.Ms)
		if r.Warning != "" || r.Plain != r.N/2 {
			t.Errorf("%d modules: %d calls get plain values, want %d, warning %q", r.N, r.Plain, r.N/2, r.Warning)
		}
	}
	// The large chunk takes about 10ms. Reading every module again for each one took 8s, so a busy
	// machine stays far under the limit and that slip does not.
	if large := runs[1]; large.Ms > 1000 {
		t.Errorf("%d modules take %.1f ms, want at most 1000", large.N, large.Ms)
	}
}

// A Vite whose bundler does not say whether an import is static or import() leaves the route
// modules of routes.tsx to the bundler: lazyRoutes resolves nothing and warns once per build.
func TestLazyRoutesWarnOnceWithoutTheKindOfImports(t *testing.T) {
	testModules(t)
	dir := t.TempDir()
	plugin := filepath.Join(dir, "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "const fs = await import('node:fs')\nconst path = await import('node:path')\n" +
		"const { lazyRoutes } = await import(process.argv[1])\n" +
		"const root = fs.realpathSync(process.argv[2])\n" +
		"const p = lazyRoutes()\n" +
		"p.configResolved({ root })\n" +
		"const warnings = []\n" +
		"const ctx = { warn: (m) => warnings.push(String(m)), resolve: async () => { throw new Error('resolve called') }, error: (m) => { throw new Error(m) } }\n" +
		"const routes = path.join(root, '.tifa', 'routes.tsx')\n" +
		"const results = []\n" +
		"for (const source of ['../app/a/route', '../app/b/layout']) results.push(await p.resolveId.handler.call(ctx, source, routes, {}))\n" +
		"process.stdout.write(JSON.stringify({ results, warnings }))\n"
	out, err := exec.Command("node", "--input-type=module", "-e", script, plugin, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Results  []any
		Warnings []string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(got.Results) != 2 || got.Results[0] != nil || got.Results[1] != nil {
		t.Errorf("results %v, want null for each import", got.Results)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "does not say whether an import is static") {
		t.Errorf("warnings %q, want one that says the bundler does not tell the kind of import", got.Warnings)
	}
}

// The imports that lead to a generated module end in route.gen, with or without .js or .ts, from
// the TSX of a directory under app/ or from a generated module. They resolve to the
// route.gen.ts of the directory in .tifa/app, whatever the path in between. A directory without
// a generated module, and a directory outside app/, fail the build.
func TestGeneratedResolvesRouteGen(t *testing.T) {
	testModules(t)
	dir := t.TempDir()
	plugin := filepath.Join(dir, "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "root")
	writeTree(t, root, map[string]string{
		"app/x/page.tsx":                    "",
		"app/x/y/page.tsx":                  "",
		"app/z/page.tsx":                    "",
		"lib/a.ts":                          "",
		".tifa/app/x/route.gen.ts":          "",
		".tifa/app/x/y/route.gen.ts":        "",
		".tifa/app/x/y/route.gen.helper.ts": "",
	})
	script := "const fs = await import('node:fs')\n" +
		"const { generated } = await import(process.argv[1])\n" +
		"const root = fs.realpathSync(process.argv[2])\n" +
		"const p = generated()\n" +
		"p.config({ root })\n" +
		"const filter = p.resolveId.filter.id\n" +
		"const ctx = { error: (m) => { throw new Error(m) } }\n" +
		"const out = {}\n" +
		"const cases = JSON.parse(process.argv[3])\n" +
		"for (const [source, importer] of cases) {\n" +
		"  const key = source + ' from ' + importer\n" +
		"  if (!filter.test(source)) { out[key] = 'unmatched'; continue }\n" +
		"  try { out[key] = p.resolveId.handler.call(ctx, source, root + '/' + importer).slice(root.length + 1) } catch (e) { out[key] = 'error: ' + e.message }\n" +
		"}\n" +
		"process.stdout.write(JSON.stringify(out))\n"
	cases := [][2]string{
		{"./route.gen", "app/x/page.tsx"},
		{"./route.gen.js", "app/x/page.tsx"},
		{"./route.gen.ts", "app/x/page.tsx"},
		{"../route.gen", "app/x/y/page.tsx"},
		{"../x/route.gen", "app/z/page.tsx"},
		{"./route.gen", ".tifa/app/x/y/route.gen.ts"},
		{"./route.gen", "app/z/page.tsx"},
		{"./route.gen", "lib/a.ts"},
		{"./+route", "app/x/page.tsx"},
		{"./+layout", "app/x/page.tsx"},
		{"./route.gen.helper", "app/x/page.tsx"},
		{"pkg/route.gen", "app/x/page.tsx"},
	}
	input, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	outBytes, err := exec.Command("node", "--input-type=module", "-e", script, plugin, root, string(input)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, outBytes)
	}
	var got map[string]string
	if err := json.Unmarshal(outBytes, &got); err != nil {
		t.Fatalf("%v: %s", err, outBytes)
	}
	want := map[string]string{
		"./route.gen from app/x/page.tsx":             ".tifa/app/x/route.gen.ts",
		"./route.gen.js from app/x/page.tsx":          ".tifa/app/x/route.gen.ts",
		"./route.gen.ts from app/x/page.tsx":          ".tifa/app/x/route.gen.ts",
		"../route.gen from app/x/y/page.tsx":          ".tifa/app/x/route.gen.ts",
		"../x/route.gen from app/z/page.tsx":          ".tifa/app/x/route.gen.ts",
		"./route.gen from .tifa/app/x/y/route.gen.ts": ".tifa/app/x/y/route.gen.ts",
		"./route.gen from app/z/page.tsx":             "error: ./route.gen: app/z has no generated route.gen module: it is made for a directory with page.tsx, layout.tsx, layout_.tsx or a Layout, or a method handler in route.go",
		"./route.gen from lib/a.ts":                   "error: ./route.gen is generated for directories under app/ only",
		"./+route from app/x/page.tsx":                "unmatched",
		"./+layout from app/x/page.tsx":               "unmatched",
		"./route.gen.helper from app/x/page.tsx":      "unmatched",
		"pkg/route.gen from app/x/page.tsx":           "unmatched",
	}
	for key, w := range want {
		if strings.HasPrefix(w, "error: ") && strings.HasPrefix(got[key], w) {
			continue
		}
		if got[key] != w {
			t.Errorf("%s = %q, want %q", key, got[key], w)
		}
	}
}
