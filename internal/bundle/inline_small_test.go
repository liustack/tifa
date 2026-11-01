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

// smallResult is what inlineSmallFunctions of the tifa Vite plugin does to a source, run on Node
// with the parser of Vite: the new source, how many calls it wrote out, the warning, whether the
// new source parses, and what the source logs and returns when it runs, before and after.
type smallResult struct {
	Code           string
	Count          int
	Warning        string
	Parses         bool
	Before, After  string
	Unresolved     []string
	UnresolvedPost []string
}

// inlineSmallOnNode runs inlineSmallFunctions on each of sources. A source defines the function
// run, which may call log: log records its arguments and returns the first. The script runs the
// source before and after in strict mode (a bundle is an ES module), and gives the log and the
// value or error of run.
func inlineSmallOnNode(t *testing.T, sources []string) []smallResult {
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
const parse = (code) => parseAst(code)
const parses = (code) => {
  try {
    return !!parseAst(code)
  } catch {
    return false
  }
}
const run = (code) => {
  const trace = []
  const log = (...args) => (trace.push(args.map(String).join(',')), args[0])
  try {
    const value = new Function('log', '"use strict";\n' + code + '\nreturn run')(log)()
    trace.push('=> ' + (typeof value === 'object' ? JSON.stringify(value) : String(value)))
  } catch (err) {
    trace.push('throws ' + err.name + ': ' + err.message)
  }
  return trace.join(' | ')
}
const out = JSON.parse(process.argv[2]).map((source) => {
  const r = plugin.inlineSmallFunctions(source, parse)
  return {
    Code: r.code,
    Count: r.count,
    Warning: r.warning ?? '',
    Parses: parses(r.code),
    Before: run(source),
    After: run(r.code),
    Unresolved: plugin.unresolvedNames(source, parse),
    UnresolvedPost: plugin.unresolvedNames(r.code, parse),
  }
})
process.stdout.write(JSON.stringify(out))
`
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("inlineSmallFunctions: %v", err)
	}
	var results []smallResult
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return results
}

// smallCase is one source and how many calls the plugin writes out in it.
type smallCase struct {
	name, source string
	want         int
	contains     []string // text the new source must have
	lacks        []string // text the new source must not have
}

func checkSmallCases(t *testing.T, cases []smallCase) {
	t.Helper()
	sources := make([]string, len(cases))
	for i, c := range cases {
		sources[i] = c.source
	}
	results := inlineSmallOnNode(t, sources)
	for i, c := range cases {
		got := results[i]
		t.Run(c.name, func(t *testing.T) {
			if got.Count != c.want {
				t.Errorf("%d calls written out, want %d:\n%s", got.Count, c.want, got.Code)
			}
			if c.want == 0 && got.Code != c.source {
				t.Errorf("the code changed though no call did:\n%s", got.Code)
			}
			if !got.Parses {
				t.Errorf("the new code does not parse:\n%s", got.Code)
			}
			if got.Before != got.After {
				t.Errorf("the code gives %q, want %q (as before):\n%s", got.After, got.Before, got.Code)
			}
			if strings.Contains(got.Before, "throws SyntaxError") {
				t.Errorf("the case itself is broken: %s", got.Before)
			}
			if strings.Count(got.Code, "\n") != strings.Count(c.source, "\n") {
				t.Errorf("the code has %d lines, want %d:\n%s", strings.Count(got.Code, "\n"), strings.Count(c.source, "\n"), got.Code)
			}
			if strings.Join(got.Unresolved, ",") != strings.Join(got.UnresolvedPost, ",") {
				t.Errorf("the names without a declaration changed from %v to %v", got.Unresolved, got.UnresolvedPost)
			}
			for _, want := range c.contains {
				if !strings.Contains(got.Code, want) {
					t.Errorf("the code has no %q:\n%s", want, got.Code)
				}
			}
			for _, bad := range c.lacks {
				if strings.Contains(got.Code, bad) {
					t.Errorf("the code has %q:\n%s", bad, got.Code)
				}
			}
		})
	}
}

// Calls of small functions are written out as the body of the function, in every form of body
// the plugin knows. Each case runs before and after and must log and give the same.
func TestPluginWritesOutCallsOfSmallFunctions(t *testing.T) {
	checkSmallCases(t, []smallCase{
		{name: "a test of one argument", want: 2, source: `
function isA(x) { return x === 'a'; }
function run() { var v = log('a'); return isA(v) + ',' + isA('b'); }
`},
		{name: "a call that forwards its arguments", want: 1, source: `
function other(a, b) { log('other', a, b); return a; }
function wrap(a, b) { return other(a, b); }
function run() { var x = 1; return wrap(x, 2); }
`, contains: []string{"other(x, 2)"}},
		{name: "a property of the argument is read once", want: 1, source: `
function getX(o) { return o.x; }
function run() { var o = { get x() { log('get'); return 5; } }; return getX(o); }
`},
		{name: "a constant and nothing", want: 2, source: `
function k() { return 42; }
function nothing() {}
function run() { return k() + String(nothing()); }
`},
		{name: "statements of assignments", want: 2, source: `
var a = 0, b = 0;
function reset() { a = 1; b = 2; }
function run() { reset(); var r = String(reset()); return a + b + r; }
`, contains: []string{"(a = 1, b = 2, void 0)"}},
		{name: "statements in a sequence that ignores their value", want: 1, source: `
var a = 0, b = 0;
function reset() { a = 1; b = 2; }
function run() { return (reset(), a + b); }
`, lacks: []string{"void 0"}},
		{name: "a chain of ifs that return", want: 3, source: `
function sign(n) { if (n < 0) return -1; if (n === 0) return 0; return 1; }
function run() { return [sign(-5), sign(0), sign(7)]; }
`},
		{name: "an if with an else that return", want: 2, source: `
function pick(c) { if (c) { return 'yes'; } else { return 'no'; } }
function run() { return pick(true) + pick(0); }
`},
		{name: "an arrow and a function expression, one inside the call of the other", want: 2, source: `
var inc = (n) => n + 1;
var twice = function (n) { return n * 2; };
function run() { return inc(twice(3)); }
`},
		{name: "arguments with effects, each used once in the order of the calls", want: 1, source: `
function pair(a, b) { return [a, b]; }
function run() { return pair(log('x'), log('y')); }
`},
		{name: "an argument with effects after reads", want: 1, source: `
var base = { n: 1 };
function add(x) { return base.n + x; }
function run() { return add(log(5)); }
`},
		{name: "a getter of the argument is called once", want: 1, source: `
function plus(v) { return v + 1; }
function run() { var o = { get g() { log('g'); return 1; } }; return plus(o.g); }
`},
		{name: "a name from a function expression", want: 2, source: `
var f = function f(a) { return a + 1; };
var h = function hh(a) { return a * 2; };
function run() { return f(2) + h(5); }
`},
		{name: "a parameter that is called keeps this empty", want: 1, source: `
function call1(fn, v) { return fn(v); }
var o = { m() { return this === undefined ? 'empty' : 'object'; } };
function run() { return call1(o.m, 1); }
`, contains: []string{"(0, o.m)(1)"}},
		{name: "the value of a call that is called keeps this empty", want: 1, source: `
function pick(o) { return o.m; }
var o = { m() { return this === undefined ? 'empty' : 'object'; } };
function run() { return pick(o)(); }
`, contains: []string{"(0, "}},
		{name: "a name written as a shorthand property", want: 1, source: `
function box(a) { return { a }; }
function run() { var s = 'z'; return box(s); }
`, contains: []string{"{ a: s }"}},
		{name: "missing and extra arguments", want: 2, source: `
function two(a, b) { return b === undefined; }
function one(a) { return a; }
function run() { return [two(1), one(1, 2)]; }
`},
		{name: "a counter of the module", want: 2, source: `
var n = 0;
function next() { return n++; }
function run() { return next() + next(); }
`},
		{name: "a name that the module does not declare, not hidden", want: 1, source: `
function seen() { return typeof nope; }
function run() { return seen(); }
`},
		{name: "a call in a method, not one in a default value", want: 1, source: `
function pred(x) { return x > 0; }
class C { m() { return pred(1); } }
function run() { var f = (a = pred(2)) => a; return new C().m() && f(); }
`},
		{name: "lines of a call", want: 1, source: `
function f(a, b) { return a +
  b; }
function run() { return f(
  1,
  2
); }
`},
		{name: "a body of several lines in a call of one", want: 1, source: `
function f(a, b) { return [a,
  b]; }
function run() { return f(1, 2); }
`},
		{name: "a function next to a stand-in of a Node module", want: 1, source: `
//#region \0tifa-node:["node:path","/pkg/index.js"]
const e0 = (p) => { throw new Error('stand-in'); };
//#endregion
//#region pkg/index.js
const read = (p) => p + 1;
//#endregion
function run() { return read(1); }
`},
		{name: "a function of the module that only runs after the constant is set", want: 1, source: `
const f = (a) => a + 1
function g() { log("g"); return f(1) }
var late = g()
function run() { return late }
`},
		{name: "functions of the module that call each other and run after the constant is set", want: 1, source: `
const f = (a) => a + 1
function g(n) { log("g"); return n ? h(n - 1) : f(1) }
function h(n) { log("h"); return g(n) }
function run() { return g(2) }
`},
		{name: "after a statement, in a branch", want: 1, source: `
var a = 0;
function setA() { a = 1; }
function run() { if (log(true)) setA(); return a; }
`},
	})
}

// Each condition of the plugin has a case in which it holds the call back.
func TestPluginKeepsCallsWhoseBodyOrWhoseCallerItCannotCopy(t *testing.T) {
	checkSmallCases(t, []smallCase{
		{name: "a name of the body hidden by a variable at the call", source: `
var k = 10;
function addk(a) { return a + k; }
function run() { var k = 20; return addk(1); }
`},
		{name: "a name from outside the module hidden by a variable at the call", source: `
function addg(a) { return a + gg; }
function run() { globalThis.gg = 5; function inner() { var gg = 7; return addg(1); } return inner(); }
`},
		{name: "a name hidden by a parameter at the call", source: `
var k = 10;
function addk(a) { return a + k; }
function run() { return (function (k) { return addk(1); })(20); }
`},
		{name: "a name hidden by a variable of a block at the call", source: `
let k = 10;
function addk(a) { return a + k; }
function run() { { let k = 20; return addk(1); } }
`},
		{name: "a name hidden by a catch parameter at the call", source: `
var k = 10;
function addk(a) { return a + k; }
function run() { try { throw 20; } catch (k) { return addk(1); } }
`},
		{name: "a name hidden by a class at the call", source: `
var k = 10;
function addk(a) { return a + k; }
function run() { return (class k { static m() { return addk(1); } }).m(); }
`},
		{name: "the callee is another variable of the same name", source: `
function f(a) { return a + 1; }
function run() { var f = log; return f(1); }
`},
		{name: "this", source: `
function th(a) { return this === undefined ? a : 0; }
function run() { return th(1); }
`},
		{name: "arguments", source: `
function ar(a) { return arguments.length + a; }
function run() { return ar(1); }
`},
		{name: "arguments of the function that made the arrow, from another function", source: `
function run() { var f = (a) => arguments.length + a; function g(x, y) { var z = 0; return f(1); } return g(5, 6); }
`},
		{name: "new.target", source: `
function nt() { return new.target; }
function run() { return String(nt()); }
`},
		{name: "a function calls itself", source: `
function fact(n) { return n <= 1 ? 1 : n * fact(n - 1); }
function run() { return fact(4); }
`},
		{name: "a function expression calls itself by its name", source: `
var f = function g(n) { return n ? g(n - 1) : 0; };
function run() { return f(3); }
`},
		{name: "a function expression returns its name", source: `
var f = function g(n) { return typeof g; };
function run() { return f(3); }
`},
		{name: "the function is set again", source: `
var f = function (a) { return a + 1; };
function run() { var r = f(1); f = function (a) { return a + 2; }; return r + f(1); }
`},
		{name: "a declaration is set again", source: `
function f(a) { return a + 1; }
function run() { var r = f(1); f = log; return r + f(1); }
`},
		{name: "the name is declared twice", source: `
var f = function (a) { return a + 1; };
var f = function (a) { return a + 2; };
function run() { return f(1); }
`},
		{name: "called before it is set", source: `
function run() { try { return g(1); } catch (e) { return e.name; } var g = function (a) { return a + 1; }; }
`},
		{name: "an argument with effects that the body does not use", source: `
function ig(a) { return 1; }
function run() { log('first'); return ig(log('x')); }
`},
		{name: "an argument with effects that the body uses twice", source: `
function dbl(a) { return a + a; }
function run() { return dbl(log(2)); }
`},
		{name: "an argument with effects used in a branch", source: `
function both(a, b) { return a && b; }
function run() { return both(1, log('b')); }
`},
		{name: "an argument with effects used in a branch of an if", source: `
function either(a, b) { if (a) return b; return 0; }
function run() { return either(0, log('b')); }
`},
		{name: "an argument with effects used after an optional chain", source: `
function opt(o, a) { return o?.[a]; }
function run() { return opt(null, log('a')); }
`},
		{name: "arguments with effects used in the other order", source: `
function swap(a, b) { return b - a; }
function run() { return swap(log(1), log(5)); }
`},
		{name: "a call in the body before the argument with effects", source: `
function pre(a) { return log('pre') + a; }
function run() { return pre(log(1)); }
`},
		{name: "a conversion in the body before the argument with effects", source: `
function co(o, a) { return (o + 1) * a; }
function run() { var o = { valueOf() { log('valueOf'); return 1; } }; return co(o, log(2)); }
`},
		{name: "an assignment in the body before the argument with effects", source: `
var n = 0;
function as(a) { return (n = 5) + a; }
function run() { return as(log(n)); }
`},
		{name: "an argument that the body assigns", source: `
function sp(a) { return a = 1; }
function run() { var z = 5; var r = sp(z); return [r, z]; }
`},
		{name: "an argument beyond the parameters, with effects", source: `
function one(a) { return a; }
function run() { log('first'); return one(1, log('x')); }
`},
		{name: "a private name inside a class that has another of that name", source: `
class C {
  #x = 'outer';
  m() {
    const get = (o) => o.#x;
    class D { #x = 'inner'; n(o) { return get(o); } }
    return new D().n(this);
  }
}
function run() { return new C().m(); }
`},
		{name: "a name that the body sets, as the argument", source: `
var n = 1;
function setn(a) { return (n = 2, a); }
function run() { return setn(n); }
`},
		{name: "an async function and a generator", source: `
async function af(a) { return a; }
function* ge(a) { yield a; }
function run() { return [typeof af(1).then, ge(2).next().value]; }
`},
		{name: "a parameter with a default, a rest parameter and a pattern", source: `
function d(a = 1) { return a; }
function r(...a) { return a; }
function p({ a }) { return a; }
function run() { return [d(), r(1), p({ a: 2 })]; }
`},
		{name: "a spread argument", source: `
function f(a) { return a; }
function run() { var xs = [1, 2]; return f(...xs); }
`},
		{name: "an optional call", source: `
function f(a) { return a + 1; }
function run() { return f?.(1); }
`},
		{name: "a body of more than 32 nodes", source: `
function big(a) { return a + 1 + 2 + 3 + 4 + 5 + 6 + 7 + 8 + 9 + 10 + 11 + 12 + 13 + 14 + 15 + 16 + 17; }
function run() { return big(1); }
`},
		{name: "an argument that the module does not declare", source: `
function f(a) { return 1; }
function run() { try { return f(missing); } catch (e) { return e.name; } }
`},
		{name: "an argument that the module does not declare, used", source: `
function f(a) { return typeof a; }
function run() { try { return f(missing); } catch (e) { return e.name; } }
`},
		{name: "a function that is also a constructor", source: `
function Box(v) { return { v }; }
function run() { return [new Box(1).v, Box(2).v]; }
`},
		{name: "a function with a prototype", source: `
function Box(v) { return { v }; }
Box.prototype.hello = function () { return 1; };
function run() { return Box(2).v; }
`},
		{name: "a function in the body", source: `
function mk(a) { return () => a; }
function run() { return mk(1)(); }
`},
		{name: "a method in the body", source: `
function om(a) { return { m() { return a; } }; }
function run() { return om(1).m(); }
`},
		{name: "a tagged template in the body", source: `
function tg(a) { return String.raw` + "`x${a}`" + `; }
function run() { return tg(1); }
`},
		{name: "an if that does not return in all of its branches", source: `
function g(a, b) { if (a) { if (b) return 1; } return 2; }
function run() { return [g(1, 0), g(0, 0), g(1, 1)]; }
`},
		{name: "a statement that is not an expression", source: `
var n = 0;
function f(a) { n = a; if (a) n = 2; }
function run() { f(1); return n; }
`},
		{name: "a comment of a line in the body, in a call of one line", source: `
function f(a, b) { return a + // note
  b; }
function run() { return f(1, 2); }
`},
		{name: "the code before the call has no semicolon", source: `
var a = 0
var b = 0
function reset() { a = 1; b = 2; }
function run() { var x = 1
reset()
return a + b }
`},
		{name: "a stand-in of a Node module", source: `
//#region \0tifa-node:["node:path","/pkg/index.js"]
const e0 = (p) => { throw new Error('stand-in'); };
//#endregion
//#region pkg/index.js
const read = (p) => e0(p);
//#endregion
function run() { return read(1); }
`},
		{name: "a default value does not see the declarations of the body", source: `
function f() { return 1 }
function run(a = f()) {
  function f() { return 2 }
  return a
}
`},
		{name: "a closure in a default value does not see the declarations of the body", want: 1, source: `
function f() { return 1 }
function run(a = () => f()) {
  var f = () => 2
  return a()
}
`},
		{name: "a call in a default value that reads a later parameter", source: `
function f(a) { return 1 }
function run(a = f(b), b = 2) { return a }
`},
		{name: "a function set in a branch that does not run", source: `
function run() {
  if (false) {
    var f = () => 1
  }
  return f()
}
`},
		{name: "a function called by a closure before the constant is set", source: `
function run() {
  function g() { log("g"); return f() }
  try { g() } catch (e) { return e.name }
  const f = () => 1
  return "ok"
}
`},
		{name: "a call after an object that ends the line before", source: `
function f() { return 2 }
function run() {
  var o = {}
  f()
  return o
}
`},
		{name: "a call after a line with a semicolon in a comment", source: `
function f() { return 2 }
function run() {
  var x = 1 // ;
  f()
  return x
}
`},
		{name: "an argument that is read before its let is set", source: `
function f(a) { return 1 }
function run() {
  return f(x)
  let x = 2
}
`},
		{name: "an argument read through a variable that a loop sets again", source: `
function f(a, b) { return [a, b, a] }
function run() {
  function* gen() {
    for (var i of [1, 2]) {
      var x = i
      yield () => f(x, it.next().done)
    }
  }
  const it = gen()
  return it.next().value()
}
`},
		{name: "a template that converts each value in its place", source: `
function f(a, b) { return ` + "`${a}${b}`" + ` }
function run() {
  var a = { toString() { log("convert"); return "a" } }
  return f(a, log("b"))
}
`},
		{name: "a computed name that converts before the value", source: `
function f(a, b) { return { [a]: b } }
function run() {
  var a = { toString() { log("convert"); return "a" } }
  return f(a, log("b"))
}
`},
		{name: "a shorthand __proto__ that sets the prototype", source: `
function box(__proto__) { return { __proto__ } }
function run() {
  var o = { x: 1 }
  return Object.hasOwn(box(o), "__proto__")
}
`},
		{name: "a shorthand __proto__ with no argument", source: `
function box(__proto__) { return { __proto__ } }
function run() { return Object.hasOwn(box(), "__proto__") }
`},
		{name: "a typeof of a call whose body reads a missing name", want: 1, contains: []string{"typeof (0, "}, source: `
function f() { return missing }
function run() { return typeof f() }
`},
		{name: "a variable of catch with a var of its name", source: `
function run() {
  try { throw 0 }
  catch (f) { var f = () => 2 }
  return f()
}
`},
		{name: "an argument that throws, +1n", source: `
function f(x) { return 1 }
function run() { return f(+1n) }
`},
		{name: "a function of the module that runs before the constant it calls is set", source: `
var early
try { early = g() } catch (e) { early = e.name }
const f = (a) => a + 1
function g() { log("g"); return f(1) }
function run() { return early }
`},
		{name: "a function of the module that another function of it calls before the constant is set", source: `
var early
function h() { log("h"); return g() }
try { early = h() } catch (e) { early = e.name }
const f = (a) => a + 1
function g() { log("g"); return f(1) }
function run() { return early }
`},
		{name: "an argument that is a later parameter, read by a closure in a default value", source: `
function f(a) { return 1 }
function run(a = (() => f(b))(), b = 2) { return a }
`},
		{name: "an argument that is a later binding of a catch pattern", source: `
function f(a) { return 1 }
function run() {
  try { throw {} }
  catch ({ a = f(b), b = 2 }) { return a }
}
`},
		{name: "an argument that is a var a generator sets after the call", source: `
function f(a, b) { return [a, b, a] }
function run() {
  function* gen() {
    yield () => f(x, it.next().done)
    var x = 2
  }
  const it = gen()
  return it.next().value()
}
`},
		{name: "a class as a property value gets no name", want: 1, contains: []string{"(0, "}, source: `
function f(a) { return { a } }
function run() {
  return f(class {
    static { log(this.name) }
  }).a.name
}
`},
		{name: "an arrow as a property value gets no name", want: 1, contains: []string{"(0, "}, source: `
function f(a) { return { a } }
function run() { return f(() => 1).a.name }
`},
		{name: "an arrow that the call passes on to a constant gets no name", want: 1, contains: []string{"(0, "}, source: `
function f(a) { return a }
function run() {
  const x = f(() => 1)
  return x.name
}
`},
		{name: "an arrow that the call assigns to a property gets no name", want: 1, contains: []string{"(0, "}, source: `
var obj = {}
function f(a) { return obj.x = a }
function run() { return f(() => 1).name }
`},
		{name: "a direct eval", source: `
function isA(x) { return x === 'a'; }
function run() { return isA('a') + eval('1'); }
`},
	})
}

// The names that no declaration of the code gives: after the change they must be the same, and
// when they are not, nothing changes and the build gets a warning.
func TestPluginKeepsTheNamesWithoutADeclaration(t *testing.T) {
	source := `
function f(a) { return 1; }
function run() { return f(undefined); }
`
	got := inlineSmallOnNode(t, []string{source})[0]
	if got.Count != 0 || got.Code != source {
		t.Errorf("count %d, the code:\n%s", got.Count, got.Code)
	}
	if !strings.Contains(got.Warning, "undefined") {
		t.Errorf("warning = %q, want it to name undefined", got.Warning)
	}
	if got.Before != got.After {
		t.Errorf("gives %q, want %q", got.After, got.Before)
	}
}

// unresolvedNames lists the names that code reads or sets and no declaration of it gives, sorted.
func TestPluginFindsTheNamesWithoutADeclaration(t *testing.T) {
	cases := []struct {
		name, source string
		want         string
	}{
		{"globals", `console.log(a); b = 1; typeof c; d++; [e] = f; ({ g } = h);`, "a,b,c,console,d,e,f,g,h"},
		{"declared in the module", `var a = 1; let b; const c = 2; function d() {} class e {} a; b; c; d; e;`, ""},
		{"parameters and the arguments", `function f(a, { b }, [c], ...d) { return [a, b, c, d, arguments, e]; }`, "e"},
		{"arrow with default values", `var f = (a, b = a + c) => b;`, "c"},
		{"function expression name", `var f = function g() { return g; }; g;`, "g"},
		{"class name inside and outside", `var C = class D { m() { return D; } }; D;`, "D"},
		{"block scope", `{ let a = 1; a; } a;`, "a"},
		{"function in a block", `{ function a() {} a(); } a;`, "a"},
		{"var in a block", `{ var a = 1; } a;`, ""},
		{"for loops", `for (let i = 0; i < 1; i++) {} i; for (const k of []) k; for (var v in {}) v; k;`, "i,k"},
		{"catch", `try {} catch (e) { e; } e; try {} catch { }`, "e"},
		{"catch with a pattern", `try {} catch ({ message }) { message; }`, ""},
		{"properties and labels", `var o = { a: 1, b() {}, get c() { return 1; } }; o.d; o?.e; a: for (;;) { break a; }`, ""},
		{"shorthand property", `var o = { a, b };`, "a,b"},
		{"computed property", `var o = { [a]: 1 }; o[b]; class C { [c]() {} static [d] = 1; }`, "a,b,c,d"},
		{"class fields and static blocks", `class C { x = y; static { var z = 1; z; w; } }`, "w,y"},
		{"switch", `switch (a) { case 1: let b = 1; b; break; default: c; } b;`, "a,b,c"},
		{"hoisted function", `f(); function f() {}`, ""},
		{"export", `var a = 1; export { a }; export { b as c };`, "b"},
		{"destructuring declaration", `var { a, b: [c], ...d } = e; a; c; d; b;`, "b,e"},
		{"default value in a pattern", `function f({ a = b, c = a }) {}`, "b"},
		{"template literal", "var x = `${a} ${b}`;", "a,b"},
		{"new.target and import.meta", `function f() { return new.target; } import.meta.url;`, ""},
		{"nested functions", `function f() { function g() { return h; } var h = 1; return g; } h;`, "h"},
		{"typeof", `typeof a === 'undefined'`, "a"},
		{"delete and in", `a in b; delete c.d;`, "a,b,c"},
	}
	sources := make([]string, len(cases))
	for i, c := range cases {
		sources[i] = c.source
	}
	results := inlineSmallOnNode(t, sources)
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := strings.Join(results[i].Unresolved, ","); got != c.want {
				t.Errorf("names = %q, want %q", got, c.want)
			}
		})
	}
}
