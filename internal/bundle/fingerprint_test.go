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

// keyPathAnalysis is what keyPathUnits of the Vite plugin gives for one source.
type keyPathAnalysis struct {
	Units    map[string]string
	Problems []string
}

// keyPathUnitsOnNode runs keyPathUnits of the Vite plugin on each of sources with the parser of
// Vite.
func keyPathUnitsOnNode(t *testing.T, sources []string) []keyPathAnalysis {
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
		"const out = JSON.parse(process.argv[2]).map((source) => {\n" +
		"  const r = plugin.keyPathUnits(parseAst(source), source)\n" +
		"  return { Units: Object.fromEntries(r.units), Problems: r.problems }\n" +
		"})\n" +
		"process.stdout.write(JSON.stringify(out))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("keyPathUnits: %v", err)
	}
	var results []keyPathAnalysis
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return results
}

// The fingerprint of a part of React's string renderer is the same for the same code written with
// other white space, other comments, other brackets, other quotes and other names and numbers on
// what the part declares, and differs for code that does something else.
func TestKeyPathFingerprintsTellCodeApart(t *testing.T) {
	for _, c := range []struct {
		name, a, b string
		same       bool
	}{
		{"white space", "function f(keyPath) {\n  return keyPath[0] ;\n}", "function f( keyPath ){return keyPath[ 0 ];}", true},
		{"comments", "function f(keyPath) {\n  g(); // one\n  return keyPath; /* two */\n}", "function f(keyPath) { g(); return keyPath; }", true},
		{"brackets", "function f(keyPath) { return (keyPath); }", "function f(keyPath) { return keyPath; }", true},
		{"quotes", `function f(keyPath) { return keyPath['a']; }`, `function f(keyPath) { return keyPath["a"]; }`, true},
		{"numbers of the compiler", "function f(keyPath) { var a$12 = keyPath, a$jscomp$3 = 1; return a$12 + a$jscomp$3 }", "function f(keyPath) { var a$99 = keyPath, a$jscomp$7 = 1; return a$99 + a$jscomp$7 }", true},
		{"other names for what the part declares", "function f(keyPath) { var saved = keyPath; return saved }", "function f(keyPath) { var other = keyPath; return other }", true},
		{"names swapped", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$1 - a$2 }", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$2 - a$1 }", false},
		{"names merged", "function f(keyPath) { var a$1 = 1, a$2 = 2; return a$1 - a$2 }", "function f(keyPath) { var a$1 = 1, a$1 = 2; return a$1 - a$1 }", false},
		{"a space in a string", `function f(keyPath) { return "a b" }`, `function f(keyPath) { return "a  b" }`, false},
		{"a plus and an increment", "function f(keyPath) { return a + +b }", "function f(keyPath) { return a++ + b }", false},
		// A line break after return ends the statement, and a letter next to a keyword makes a name.
		{"a line break after return", "function f(keyPath) { return keyPath; }", "function f(keyPath) {\n  return\n  keyPath;\n}", false},
		{"a letter next to return", "function f(keyPath) { return α; }", "function f(keyPath) { returnα; }", false},
		{"a line break after a name", "function f(keyPath) { var a = keyPath\n(1) }", "function f(keyPath) { var a = keyPath; (1) }", false},
		// A property name and a name no declaration gives are not renamed, and a name that an outer
		// declaration gives is not one that nothing gives.
		{"another property name", "function f(keyPath) { return keyPath.a$1; }", "function f(keyPath) { return keyPath.a$2; }", false},
		{"another global", "function f(keyPath) { return a$1; }", "function f(keyPath) { return a$2; }", false},
		{"another outer name", "var a$1, a$2; function f(keyPath) { return a$1; }", "var a$1, a$2; function f(keyPath) { return a$2; }", false},
		{"a global that an outer name hides", "function f(keyPath) { return a; }", "var a; function f(keyPath) { return a; }", false},
		{"another read", "function f(keyPath) { return keyPath[0] }", "function f(keyPath) { return keyPath[1] }", false},
		{"a new read", "function f(keyPath) { return keyPath }", "function f(keyPath) { return keyPath.length }", false},
	} {
		got := keyPathUnitsOnNode(t, []string{c.a, c.b})
		if got[0].Units["f"] == "" || got[1].Units["f"] == "" {
			t.Errorf("%s: parts %v and %v, want f in each", c.name, got[0].Units, got[1].Units)
			continue
		}
		if same := got[0].Units["f"] == got[1].Units["f"]; same != c.same {
			t.Errorf("%s: same fingerprint = %v, want %v", c.name, same, c.same)
		}
	}
}

// keyPathParts runs keyPathUnits on source and returns the sorted names of its parts and its problems.
func keyPathParts(t *testing.T, source string) ([]string, []string) {
	t.Helper()
	r := keyPathUnitsOnNode(t, []string{source})[0]
	names := make([]string, 0, len(r.Units))
	for name := range r.Units {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, r.Problems
}

// The parts that see a key path are those with keyPath in a name, a string or a template, and those
// that a key path reaches: found through the names of functions, the aliases of a name, the methods
// of an object and the functions a function calls, and through the variables, the returns and the
// containers a key path goes through.
func TestKeyPathFingerprintsFindTheParts(t *testing.T) {
	for _, c := range []struct {
		name, source string
		want         []string
	}{
		{"a function and what it calls", "function g(x) { return x.length }\nfunction h() {}\nfunction f(keyPath) { return g(keyPath) }", []string{"f", "g"}},
		{"a call with a property", "function g(x) {}\nfunction f(task) { g(task.keyPath) }", []string{"f", "g"}},
		{"a call that passes a part of one", "function g(x) {}\nfunction f(keyPath) { g(keyPath[0]) }", []string{"f", "g"}},
		{"a call that passes a name from one", "function g(x) {}\nfunction f(keyPath) { g(keyPath[1]) }", []string{"f"}},
		{"a call that passes an array that holds one", "function g(x) {}\nfunction f(keyPath) { g([keyPath, 1]) }", []string{"f", "g"}},
		{"a call that passes nothing of one", "function g(x) {}\nfunction f(keyPath) { g(1) }", []string{"f"}},
		// What a review found the one step of the first version missed.
		{"a function under another name", "function f(keyPath) { return alias(keyPath) }\nvar alias = g;\nfunction g(x) { return x.length }", []string{"f", "g"}},
		{"a method of an object", "function f(keyPath) { return obj.g(keyPath) }\nvar obj = { g(x) { return x.length } };", []string{"f", "obj"}},
		{"a method that is a function value", "function f(keyPath) { return obj.g(keyPath) }\nvar obj = { g: function (x) { return x.length } };", []string{"f", "obj"}},
		{"a function that calls a function that calls one", "function f(keyPath) { return g(keyPath) }\nfunction g(x) { return h(x) }\nfunction h(x) { return x.length }", []string{"f", "g", "h"}},
		{"a function through two names", "function f(keyPath) { return b(keyPath) }\nvar a = h;\nvar b = a;\nfunction h(x) { return x.length }", []string{"f", "h"}},
		{"a variable that holds one", "function g(x) { return x.length }\nfunction f(keyPath) { var kept = keyPath; return g(kept) }", []string{"f", "g"}},
		{"a variable set again to another value", "function g(x) { return x.length }\nfunction f(keyPath, task) { var v = keyPath; v = task.size; return g(v) }", []string{"f"}},
		{"a variable set again in a condition", "function g(x) { return x.length }\nfunction f(keyPath, task, c) { var v = keyPath; if (c) v = task.size; return g(v) }", []string{"f", "g"}},
		{"a variable set again in a loop", "function g(x) { return x.length }\nfunction f(keyPath, list) { var v = 0; for (var i = 0; i < 2; i++) { g(v); v = keyPath } }", []string{"f", "g"}},
		{"a function that returns one", "function pick(task) { return task.keyPath }\nfunction g(x) { return x.length }\nfunction f(task) { return g(pick(task)) }", []string{"f", "g", "pick"}},
		{"a key path in a Map", "var seen = new Map();\nfunction put(keyPath) { seen.set(keyPath, 1) }\nfunction use(x) { return seen.get(x) }\nfunction other() { return seen }", []string{"other", "put", "seen", "use"}},
		{"a key path in a property of another name", "function put(task) { task.saved = task.keyPath }\nfunction read(task) { return task.saved }", []string{"put", "read"}},
		{"a key path stored through a pattern", "function f(keyPath, task) { [task.saved] = [keyPath] }\nfunction r(task) { return task.saved }", []string{"f", "r"}},
		{"a key path in an object under another name", "function g(x) { return x.length }\nfunction f(keyPath) { var o = { p: keyPath }; return g(o) }", []string{"f", "g"}},
		// A name with keyPath in it that is a property, a string or a template.
		{"a string name", "function f(task) { return task['keyPath'] }", []string{"f"}},
		{"a destructuring key in a string", "function f(task) { var { 'keyPath': p } = task; return p }", []string{"f"}},
		{"a destructuring key", "function f(task) { var { keyPath: p } = task; return p }", []string{"f"}},
		{"a template", "function f(task) { return task[`keyPath`] }", []string{"f"}},
		{"a destructured key path that is passed on", "function g(x) { return x.length }\nfunction f(task) { var { 'keyPath': p } = task; return g(p) }", []string{"f", "g"}},
		// The parts at the top of a file.
		{"one declarator of a list", "var p = null, currentlyRenderingKeyPath = null, q = 1", []string{"currentlyRenderingKeyPath"}},
		{"a name twice", "var a = keyPath\nvar a = keyPath[0]", []string{"a", "a#"}},
		{"a statement that is no declaration", "task.keyPath = null", []string{"ExpressionStatement"}},
		{"a name inside a function", "function f() { function keyPathReader() {} }", []string{"f"}},
		{"no key path", "function f(a) { return a }", nil},
	} {
		got, problems := keyPathParts(t, c.source)
		if !slices.Equal(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%s: parts %v, want %v", c.name, got, c.want)
		}
		if len(problems) != 0 {
			t.Errorf("%s: problems %v", c.name, problems)
		}
	}
}

// A key path that reaches something the plugin cannot follow is a problem, and the file is not one
// whose key paths it can leave out.
func TestKeyPathFingerprintsRefuseWhatTheyCannotFollow(t *testing.T) {
	for _, c := range []struct{ name, source, problem string }{
		{"a function that is a parameter", "function f(keyPath, call) { return call(keyPath) }", "call"},
		{"a method of an unknown object", "function f(keyPath, o) { return o.take(keyPath) }", "o.take"},
		{"a method that the file defines for a store", "var o = { push(x) { return x } };\nfunction f(keyPath, list) { list.push(keyPath) }", "list.push"},
		{"a call with apply", "function g(x) {}\nfunction f(keyPath) { return g.apply(null, [keyPath]) }", "g.apply"},
		{"a call of a call", "function f(keyPath, make) { return make()(keyPath) }", "make()"},
		{"a function set twice", "var g = function (x) {}; g = function (x) { return x.length };\nfunction f(keyPath) { return g(keyPath) }", "g"},
		{"a spread", "function g(x) {}\nfunction f(keyPath) { return g(...[keyPath]) }", "spread"},
		{"a function that reads arguments", "function g() { return arguments[0].length }\nfunction f(keyPath) { return g(keyPath) }", "reads arguments"},
		{"a store in a property computed at run time", "function f(keyPath, task, name) { task[name] = keyPath }", "task[name]"},
		{"a with statement", "function f(keyPath, o) { with (o) { return keyPath } }", "with"},
		{"a direct eval", "function f(keyPath) { return eval('keyPath') }", "eval"},
	} {
		_, problems := keyPathParts(t, c.source)
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, c.problem) }) {
			t.Errorf("%s: problems %v, want one with %q", c.name, problems, c.problem)
		}
	}
}
