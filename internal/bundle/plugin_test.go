package bundle

import (
	"encoding/json"
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
		"  const { code, switches } = plugin.guardSwitches(source, parseAst(source))\n" +
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
