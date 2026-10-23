package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/ssr"
	"github.com/liustack/tifa/js"
)

// rewritten is what rewriteStatements of the tifa Vite plugin does to a source, run on Node with
// the parser of Vite: the new source, whether it parses, and what the function f of the source
// logs and throws for each of rewriteInputs, before and after. Bad is the first place where the
// source map of the new source points to other code than the map of the old one.
type rewritten struct {
	Code          string
	Parses        bool
	Before, After []string
	Bad           string
}

// rewriteInputs is what the script passes to f: values that are false and true for a condition.
const rewriteInputs = "[0, 1, '', 'a', null, undefined, NaN, {}, [], -0, 0n, 2n]"

// rewriteOnNode runs rewriteStatements on each of sources. For every source it also builds the
// map of a bundler that wrote it with one segment at each token that points to the same place of
// an earlier source, so the map after the change can be checked token by token.
func rewriteOnNode(t *testing.T, sources []string) []rewritten {
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
const parses = (code) => {
  try {
    return !!parseAst(code)
  } catch {
    return false
  }
}
const run = (code) => {
  if (!/function f\(/.test(code)) return []
  const trace = []
  let f
  try {
    f = new Function('log', code + '\nreturn f')((...args) => trace.push(args.map(String).join(',')))
  } catch (err) {
    return ['does not compile: ' + err.message]
  }
  return ` + rewriteInputs + `.map((x) => {
    trace.length = 0
    let thrown = ''
    try {
      f(x)
    } catch (err) {
      thrown = ' throws ' + err.message
    }
    return trace.join(' ') + thrown
  })
}
const base64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'
const encode = (n) => {
  let v = n < 0 ? -2 * n + 1 : 2 * n
  let s = ''
  do {
    let d = v & 31
    v = Math.floor(v / 32)
    if (v > 0) d |= 32
    s += base64[d]
  } while (v > 0)
  return s
}
const decode = (segment) => {
  const values = []
  let value = 0
  let shift = 0
  for (const char of segment) {
    const digit = base64.indexOf(char)
    value += (digit & 31) * 2 ** shift
    if (digit & 32) {
      shift += 5
      continue
    }
    values.push(value % 2 ? -(value - 1) / 2 : value / 2)
    value = shift = 0
  }
  return values
}
// A token is a word, a number or one punctuation character. The map has a segment at each, and
// points to the same line and column of the source.
const token = /[\w$]+|\S/g
const identityMap = (code) => {
  // Source line and column are relative to the segment before, on all lines.
  let sourceLine = 0
  let sourceColumn = 0
  return code.split('\n').map((text, line) => {
    let column = 0
    return [...text.matchAll(token)].map((m) => {
      const segment = encode(m.index - column) + 'A' + encode(line - sourceLine) + encode(m.index - sourceColumn)
      column = sourceColumn = m.index
      sourceLine = line
      return segment
    }).join(',')
  }).join(';')
}
// checkMap walks the decoded map of newCode and reports the first segment whose token is not the
// token of the old code at the place it points to. The characters of an operator the change
// replaces may differ.
const replaced = new Set(['&', '|', '?', ':', '!', ';', ',', '0', '1'])
const checkMap = (oldCode, newCode, mappings) => {
  const oldLines = oldCode.split('\n')
  const newLines = newCode.split('\n')
  let sourceLine = 0
  let sourceColumn = 0
  let i = 0
  for (const text of mappings.split(';')) {
    let column = 0
    for (const segment of text.split(',')) {
      if (!segment) continue
      const fields = decode(segment)
      column += fields[0]
      sourceLine += fields[2]
      sourceColumn += fields[3]
      const got = newLines[i].slice(column).match(token)?.[0]
      const want = oldLines[sourceLine].slice(sourceColumn).match(token)?.[0]
      if (got !== want && !replaced.has(want?.[0])) return 'line ' + (i + 1) + ' column ' + (column + 1) + ' is ' + got + ', the map says ' + want
    }
    i++
  }
  return ''
}
const results = JSON.parse(process.argv[2]).map((source) => {
  const map = identityMap(source)
  const out = plugin.rewriteStatements(source, parseAst, map)
  return {
    code: out.code,
    parses: parses(out.code),
    before: run(source),
    after: run(out.code),
    bad: out.mappings === undefined ? 'no map' : checkMap(source, out.code, out.mappings),
  }
})
process.stdout.write(JSON.stringify(results))
`
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rewriteStatements: %v\n%s", err, stderr.String())
	}
	var results []rewritten
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("rewriteStatements: %v: %s", err, out)
	}
	return results
}

// rewriteCases are sources and what rewriteStatements makes of them. The want is the new source,
// "" for one that stays as it is. A source that defines f also runs before and after, and must log
// and throw the same.
var rewriteCases = []struct{ name, source, want string }{
	{"and", `function f(x){x&&log("a")}`, `function f(x){if(x){log("a")}}`},
	{"and with a semicolon", `function f(x){x&&log("a");log("b")}`, `function f(x){if(x){log("a")}log("b")}`},
	{"or", `function f(x){x||log("a");log("b")}`, `function f(x){if(!(x)){log("a")}log("b")}`},
	{"conditional", `function f(x){x?log(1):log(2)}`, `function f(x){if(x){log(1)}else{log(2)}}`},
	{"conditional chain", `function f(x){x?log(1):x===0?log(2):log(3)}`, `function f(x){if(x){log(1)}else{if(x===0){log(2)}else{log(3)}}}`},
	{"conditional in a conditional", `function f(x){x?x===1?log(1):log(2):log(3)}`, `function f(x){if(x){if(x===1){log(1)}else{log(2)}}else{log(3)}}`},
	{"and or", `function f(x){x&&log(1)||log(2)}`, `function f(x){if(!(x&&log(1))){log(2)}}`},
	{"or and", `function f(x){x||log(1)&&log(2)}`, `function f(x){if(!(x)){if(log(1)){log(2)}}}`},
	{"and and", `function f(x){x&&log(1)&&log(2)}`, `function f(x){if(x&&log(1)){log(2)}}`},
	{"or or", `function f(x){x||log(1)||log(2)}`, `function f(x){if(!(x||log(1))){log(2)}}`},
	{"or with and inside", `function f(x){log(0)||x&&log(1)}`, `function f(x){if(!(log(0))){if(x){log(1)}}}`},
	{"logical test of a conditional", `function f(x){x&&log(1)?log(2):log(3)}`, `function f(x){if(x&&log(1)){log(2)}else{log(3)}}`},
	{"comma", `function f(x){log(0),x&&log(1),log(2)}`, `function f(x){log(0);if(x){log(1)}log(2)}`},
	{"comma in a branch of an if", `function f(x){if(x)log(0),x&&log(1);else log(2)}`, `function f(x){if(x){log(0);if(x){log(1)};}else log(2)}`},
	{"comma at the end", `function f(x){log(0),x&&log(1);log(2)}`, `function f(x){log(0);if(x){log(1)}log(2)}`},
	{"comma of two", `function f(x){x||log(1),x||log(2)}`, `function f(x){if(!(x)){log(1)}if(!(x)){log(2)}}`},
	{"comma without a branch", `function f(x){log(0),log(1)}`, ""},
	{"comma in parentheses", `function f(x){x&&(log(1),log(2))}`, `function f(x){if(x){(log(1),log(2))}}`},
	{"comma element in parentheses", `function f(x){log(0),(x&&log(1)),log(2)}`, ""},
	{"comma before a function", `function f(x){x&&log(1),function(){log(2)}()}`, ""},
	{"function after and", `function f(x){x&&function(){log(1)}()}`, ""},
	{"object after and", `function f(x){log(0);x&&{a:1}.a}`, ""},
	{"object after a conditional", `function f(x){x?log(1):{a:1}.a}`, ""},
	{"class after and", `function f(x){x&&class{}}`, ""},
	{"arrow after and", `function f(x){x&&(()=>log(1))()}`, `function f(x){if(x){(()=>log(1))()}}`},
	{"label", `function f(x){a:x&&log(1);log(2)}`, `function f(x){a:{if(x){log(1)};}log(2)}`},
	{"break out of a label", `function f(x){a:{x&&log(1);if(x)break a;log(2)}}`, `function f(x){a:{if(x){log(1)}if(x)break a;log(2)}}`},
	{"else", `function f(x){if(x)x&&log(1);else log(2)}`, `function f(x){if(x){if(x){log(1)};}else log(2)}`},
	{"else without a semicolon", "function f(x){if(x)x&&log(1)\nelse log(2)}", "function f(x){if(x){if(x){log(1)}}\nelse log(2)}"},
	{"else of the branch", `function f(x){if(x)x||log(1);else log(2)}`, `function f(x){if(x){if(!(x)){log(1)};}else log(2)}`},
	{"loop", `function f(x){for(var i=0;i<2;i++)x&&log(i)}`, `function f(x){for(var i=0;i<2;i++){if(x){log(i)}}}`},
	{"while", `function f(x){var i=0;while(i++<2)x||log(i)}`, `function f(x){var i=0;while(i++<2){if(!(x)){log(i)}}}`},
	{"do", `function f(x){var i=0;do x&&log(i);while(i++<1)}`, `function f(x){var i=0;do {if(x){log(i)};}while(i++<1)}`},
	{"switch case", `function f(x){switch(x){case 1:x&&log(1);break;default:x||log(2)}}`, `function f(x){switch(x){case 1:if(x){log(1)}break;default:if(!(x)){log(2)}}}`},
	{"no semicolon", "function f(x){\n  x&&log(1)\n  log(2)\n}", "function f(x){\n  if(x){log(1)}\n  log(2)\n}"},
	{"no semicolon before a parenthesis", "function f(x){\n  x&&log(1)\n  ;(x||log(2))\n}", "function f(x){\n  if(x){log(1)\n  }(x||log(2))\n}"},
	{"newlines around the operator", "function f(x){\n  x\n    &&\n  log(1);\n}", "function f(x){\n  if(x\n    ){\n  log(1)}\n}"},
	{"comments around the operator", "function f(x){x/*&&*/&&/*||*/log(1);log(2)}", "function f(x){if(x/*&&*/){/*||*/log(1)}log(2)}"},
	{"line comment around the operator", "function f(x){x // &&\n&& log(1) // ;\n;log(2)}", "function f(x){if(x // &&\n){ log(1) // ;\n}log(2)}"},
	{"comment before the semicolon", "function f(x){x&&log(1) /* ; */ ;log(2)}", "function f(x){if(x){log(1) /* ; */ }log(2)}"},
	{"parentheses around operands", `function f(x){(x||0)&&(log(1))}`, `function f(x){if((x||0)){(log(1))}}`},
	{"parentheses around the statement", `function f(x){(x&&log(1))}`, ""},
	{"parenthesized test", `function f(x){(x)?(log(1)):(log(2))}`, `function f(x){if((x)){(log(1))}else{(log(2))}}`},
	{"colon in a string", `function f(x){x?log(":?"):log("?:")}`, `function f(x){if(x){log(":?")}else{log("?:")}}`},
	{"colon in a nested conditional", `function f(x){x?log(x?1:2):log(3)}`, `function f(x){if(x){log(x?1:2)}else{log(3)}}`},
	{"statement of a function in the branch", `function f(x){x&&[1].forEach(function(){x||log(1)})}`, `function f(x){if(x){[1].forEach(function(){if(!(x)){log(1)}})}}`},
	{"nested functions", `function f(x){function g(){x&&log(1)}g();x?g():log(2)}`, `function f(x){function g(){if(x){log(1)}}g();if(x){g()}else{log(2)}}`},
	{"arrow with a block", `function f(x){(()=>{x&&log(1)})()}`, `function f(x){(()=>{if(x){log(1)}})()}`},
	{"arrow with an expression is not a statement", `function f(x){(()=>x&&log(1))()}`, ""},
	{"method", `function f(x){({m(){x&&log(1)}}).m()}`, `function f(x){({m(){if(x){log(1)}}}).m()}`},
	{"class static block", `function f(x){class A{static{x&&log(1)}}}`, `function f(x){class A{static{if(x){log(1)}}}}`},
	{"directive", `function f(x){"use strict";x&&log(1)}`, `function f(x){"use strict";if(x){log(1)}}`},
	{"directive alone", `function f(x){"use strict"}`, ""},
	{"top level", `var x=1;x&&log(1);x||log(2);x?log(3):log(4)`, ""},
	{"nullish", `function f(x){x??log(1)}`, ""},
	{"assignment", `function f(x){x&&(x=log(1))}`, `function f(x){if(x){(x=log(1))}}`},
	{"logical assignment", `function f(x){x&&=log(1)}`, ""},
	{"a call and a value", `function f(x){x&&log(1);return x||log(2)}`, `function f(x){if(x){log(1)}return x||log(2)}`},
	{"return value", `function f(x){return x&&log(1)}`, ""},
	{"template", "function f(x){x&&log(`${x?1:2}`)}", "function f(x){if(x){log(`${x?1:2}`)}}"},
	{"regexp with the operator", `function f(x){x&&log(/&&|\?|:/.test("&&"))}`, `function f(x){if(x){log(/&&|\?|:/.test("&&"))}}`},
	{"not zero", `function f(x){log(!0,!1)}`, `function f(x){log(true,false)}`},
	{"not zero after a word", `function f(x){return!0}`, `function f(x){return true}`},
	{"not zero after typeof", `function f(x){log(typeof!0,void!1,[]instanceof!0&&0)}`, `function f(x){log(typeof true,void false,[]instanceof true&&0)}`},
	{"not zero before a word", `function f(x){log(!0 in{},!1 instanceof Object)}`, `function f(x){log(true in{},false instanceof Object)}`},
	{"not zero in a statement", `function f(x){!0&&log(1)}`, `function f(x){if(true){log(1)}}`},
	{"not zero in a template", "function f(x){log(`!0${!1}`)}", "function f(x){log(`!0${false}`)}"},
	{"not zero in strings and regexps", `function f(x){log("!0",'!1',/!0/.source,/[!1]/.source)}`, ""},
	{"not zero in a comment", "function f(x){/* !0 */log(1)// !1\n}", ""},
	{"not zero as a key", `function f(x){log(({"!0":1})["!0"],({"!1":2}).a,x.b)}`, ""},
	{"not other numbers", `function f(x){log(!2,!0.5,! 0,!(0),!00,!0n,!x,!"0")}`, ""},
	{"not zero twice", `function f(x){log(!!0,!!1,!!!0)}`, `function f(x){log(!true,!false,!!true)}`},
	{"not zero as an operand", `function f(x){log(!0+1,2*!1,[!0][0],{a:!1}.a,!0?1:2)}`, `function f(x){log(true+1,2*false,[true][0],{a:false}.a,true?1:2)}`},
	{"unicode before the change", "function f(x){log('\U0001F642 é');x&&log('\U0001F642');log(!0)}", "function f(x){log('\U0001F642 é');if(x){log('\U0001F642')}log(true)}"},
	{"nothing to change", `function f(x){if(x)log(1);else log(2);return x}`, ""},
}

// rewriteStatements changes the statements QuickJS compiles badly into forms it compiles well, and
// nothing else. Each change keeps the evaluation order and the lines, and the source map points
// at the same tokens as before.
func TestPluginRewritesStatements(t *testing.T) {
	sources := make([]string, len(rewriteCases))
	for i, c := range rewriteCases {
		sources[i] = c.source
	}
	for i, got := range rewriteOnNode(t, sources) {
		c := rewriteCases[i]
		want := c.want
		if want == "" {
			want = c.source
		}
		if got.Code != want {
			t.Errorf("%s:\n source %q\n got    %q\n want   %q", c.name, c.source, got.Code, want)
		}
		if !got.Parses {
			t.Errorf("%s: the new source does not parse: %q", c.name, got.Code)
		}
		if strings.Join(got.Before, "\n") != strings.Join(got.After, "\n") {
			t.Errorf("%s: f does not do the same:\n before %q\n after  %q", c.name, got.Before, got.After)
		}
		if got.Bad != "" {
			t.Errorf("%s: the source map of %q: %s", c.name, got.Code, got.Bad)
		}
	}
}

// condEntry is an SSR entry whose page throws inside a statement of the form flag && f(). The
// minifier keeps the form, and the tifa plugin changes it to an if statement after it. The
// client entry has the same forms and a !0.
const condEntry = "import './runtime/host.js'\n" +
	"import { renderToString } from 'react-dom/server.edge'\n" +
	"import { createElement as h } from 'react'\n" +
	"const flag = JSON.parse('true') as boolean\n" +
	"function Page() {\n" +
	"  const state = { n: 0 }\n" +
	"  state.n++\n" +
	"  flag && ((): never => { throw new Error('COND') })()\n" +
	"  state.n++\n" +
	"  try { state.n++ } finally { state.n-- }\n" +
	"  return null\n" +
	"}\n" +
	";(globalThis as any).startRender = () => {\n" +
	"  try {\n" +
	"    ;(globalThis as any).renderResult = '<' + renderToString(h(Page))\n" +
	"  } catch (error: any) {\n" +
	"    ;(globalThis as any).renderResult = JSON.stringify({ error: `${error.name}: ${error.message}\\n${error.stack}` })\n" +
	"  }\n" +
	"}\n"

const condClient = "const flag = JSON.parse('true') as boolean\n" +
	"flag && console.log('client', !0, !1)\n"

// The SSR bundle has the statements in the form QuickJS compiles well, in React DOM and in the
// application, the browser bundle keeps what the minifier wrote, and a stack trace of an error
// thrown in a changed statement names the place of the throw in the source.
func TestSSRBundleHasRewrittenStatements(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{
		filepath.Join(".tifa", "server.ts"): condEntry,
		filepath.Join(".tifa", "client.ts"): condClient,
	})
	buildApp(t, root)
	dist := filepath.Join(root, ".tifa", "dist")
	ssrJS, err := os.ReadFile(filepath.Join(dist, "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	// What the minifier writes is flag&&(()=>{throw ...})(). The changed statement is an if
	// statement around the call.
	if !regexp.MustCompile("if\\(\\w+\\)\\{\\(\\(\\)=>\\{throw Error\\(`COND`\\)\\}\\)\\(\\)\\}").Match(ssrJS) {
		i := strings.Index(string(ssrJS), "COND")
		t.Errorf("the statement that throws COND is no if statement:\n%s", ssrJS[max(0, i-100):i+60])
	}

	// Nothing is left for a second run, so every statement React DOM and the bundle's other code
	// have in that form is changed.
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "import fs from 'node:fs'\n" +
		"const plugin = await import(process.argv[1])\n" +
		"const { parseAst } = await import('vite')\n" +
		"process.stdout.write(String(plugin.rewriteStatements(fs.readFileSync(process.argv[2], 'utf8'), parseAst).count))\n"
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, filepath.Join(dist, "ssr.js"))
	cmd.Dir = testdata(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "0" {
		t.Errorf("a second run changes %s more places of the SSR bundle, want 0", out)
	}

	// The browser bundle is what the minifier wrote.
	assets, err := filepath.Glob(filepath.Join(dist, "assets", "*.js"))
	if err != nil || len(assets) == 0 {
		t.Fatalf("no scripts in the browser bundle (%v)", err)
	}
	client := ""
	for _, asset := range assets {
		code, err := os.ReadFile(asset)
		if err != nil {
			t.Fatal(err)
		}
		client += string(code)
	}
	if !strings.Contains(client, "&&console.log(`client`,!0,!1)") || strings.Contains(client, "if(") {
		t.Errorf("the browser bundle is not what the minifier wrote:\n%s", client)
	}

	// The throw is at the line and column of its new Error in the source.
	_, err = appPool(t, root).Render(context.Background(), ssr.Input{})
	if err == nil {
		t.Fatal("the render does not fail")
	}
	lines := strings.Split(condEntry, "\n")
	line := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "COND") })
	want := fmt.Sprintf(`\.tifa/server\.ts:%d:%d\)`, line+1, strings.Index(lines[line], "Error('COND')")+1)
	if !regexp.MustCompile(want).MatchString(err.Error()) {
		t.Errorf("the error does not name %s:\n%v", want, err)
	}
}
