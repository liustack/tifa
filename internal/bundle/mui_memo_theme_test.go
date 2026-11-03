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

// memoThemePage calls memoTheme of @mui/system, as its ES module and as its CommonJS file, with
// themes of the same and of other content, and renders MUI's ThemeProvider twice with one theme,
// with two nested themes and with a theme of CSS variables. For each case it records how many
// times the style function ran and what memoTheme gave or the page rendered.
const memoThemePage = `import './host'
import { createElement as h } from 'react'
import { renderToString } from 'react-dom/server.edge'
import { unstable_memoTheme as esmMemo } from '@mui/system'
import cjsMemo from './memo.cjs'
import { ThemeProvider, createTheme, styled } from '@mui/material/styles'
import Button from '@mui/material/Button'

type Memo = (fn: (arg: { theme: any }) => any) => (props: { theme: any }) => any
const out: { name: string; runs: number; out: string[] }[] = []

const palette = { main: 'red' }
const spacing = 8
const A = { palette, spacing }
const B = { palette: { main: 'green' }, spacing: 4 }
let fresh = 0
const another = () => ({ palette: { main: 'c' + ++fresh }, spacing: fresh })

function direct(memo: Memo, form: string, name: string, calls: (call: (theme: any) => void, other: (theme: any) => void) => void) {
  let runs = 0
  const style = ({ theme }: { theme: any }) => {
    runs++
    if (theme?.boom === true || (theme?.boomOnce === true && runs === 1)) throw new Error('boom')
    return { color: theme?.palette?.main ?? 'none', margin: theme?.spacing ?? 0, ...(theme?.extra ? { padding: theme.extra } : {}) }
  }
  const f = memo(style)
  const g = memo(() => ({ color: 'gray' }))
  const record: string[] = []
  const call = (theme: any) => {
    try {
      record.push(JSON.stringify(f({ theme }).style))
    } catch (e) {
      record.push('throws ' + (e as Error).message)
    }
  }
  calls(call, (theme) => g({ theme }))
  out.push({ name: form + ': ' + name, runs, out: record })
}

for (const [form, memo] of [['esm', esmMemo], ['cjs', cjsMemo]] as [string, Memo][]) {
  direct(memo, form, 'the same object', (call) => { call(A); call(A) })
  direct(memo, form, 'a copy', (call) => { call(A); call({ ...A }) })
  direct(memo, form, 'copies on three renders', (call) => { call(A); call({ ...A }); call({ ...A }) })
  direct(memo, form, 'a changed value', (call) => { call(A); call({ ...A, palette: { main: 'blue' } }) })
  direct(memo, form, 'a key added', (call) => { call(A); call({ ...A, extra: 2 }) })
  direct(memo, form, 'a key removed', (call) => { call(A); call({ palette }) })
  direct(memo, form, 'another prototype', (call) => { call(A); call(Object.assign(Object.create({ inherited: 1 }), A)) })
  direct(memo, form, 'no prototype', (call) => { call(A); call(Object.assign(Object.create(null), A)) })
  direct(memo, form, 'keys in another order', (call) => { call(A); call({ spacing, palette }) })
  direct(memo, form, 'a hidden key', (call) => { call(Object.defineProperty({ ...A }, 'hidden', { value: 1 })); call({ ...A, hidden: 1 }) })
  direct(memo, form, 'a symbol key', (call) => { const s = { ...A, [Symbol.for('k')]: 1 }; call(s); call({ ...s }) })
  direct(memo, form, 'a getter', (call) => {
    const g = { get palette() { return palette }, spacing }
    call(g)
    call(Object.defineProperties({}, Object.getOwnPropertyDescriptors(g)))
  })
  direct(memo, form, 'back and forth', (call) => { call(A); call(B); call({ ...A }); call({ ...B }) })
  direct(memo, form, 'no theme', (call) => { call(undefined); call(undefined) })
  direct(memo, form, 'a style function that throws', (call) => { const t = { ...A, boom: true }; call(t); call({ ...t }) })
  direct(memo, form, 'a style function that throws once', (call) => { const t = { ...A, boomOnce: true }; call(t); call({ ...t }); call({ ...t }) })
  direct(memo, form, 'seven other themes', (call, other) => {
    const t = another()
    call(t)
    for (let i = 0; i < 7; i++) other(another())
    call({ ...t })
  })
  direct(memo, form, 'eight other themes', (call, other) => {
    const t = another()
    call(t)
    for (let i = 0; i < 8; i++) other(another())
    call({ ...t })
  })
}

function rendered(name: string, tree: (Box: any) => any) {
  let runs = 0
  const Box = styled('div')(esmMemo(({ theme }: { theme: any }) => {
    runs++
    return { color: theme.palette.primary.main, padding: theme.spacing(1) }
  }))
  const record = [renderToString(tree(Box)), renderToString(tree(Box))]
  out.push({ name, runs, out: record })
}

const light = createTheme({ palette: { primary: { main: '#123456' } } })
const dark = createTheme({ palette: { primary: { main: '#654321' } } })
const vars = createTheme({ cssVariables: true, palette: { primary: { main: '#0a0b0c' } } })
rendered('one ThemeProvider', (Box) => h(ThemeProvider, { theme: light }, h(Box, null, 'a'), h(Button, null, 'Go')))
rendered('nested ThemeProviders', (Box) =>
  h(ThemeProvider, { theme: light }, h(Box, null, 'a'), h(ThemeProvider, { theme: dark }, h(Box, null, 'b'), h(Button, null, 'Go')), h(Box, null, 'c')),
)
rendered('CSS variables', (Box) => h(ThemeProvider, { theme: vars }, h(Box, null, 'a'), h(Button, null, 'Go')))

;(globalThis as any).result = JSON.stringify(out)
`

type memoThemeCase struct {
	Name string
	Runs int
	Out  []string
}

// withMUI returns a copy of testdata/ssr with copies of @mui/system and @mui/material, whose
// memoTheme.mjs and memoTheme.js change has changed, if it is not nil. The other packages are the
// test's. Material is copied too, so that it imports the copy of @mui/system.
func withMUI(t *testing.T, change func(string) string) string {
	t.Helper()
	root := copyTestdata(t, "ssr", false)
	modules := testModules(t)
	link := func(from, to string) {
		if _, err := os.Lstat(to); err == nil {
			return
		}
		real, err := filepath.EvalSymlinks(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, to); err != nil {
			t.Fatal(err)
		}
	}
	// linkAll links each package in dir, the members of a scope one by one, into root.
	linkAll := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if !strings.HasPrefix(e.Name(), "@") {
				link(filepath.Join(dir, e.Name()), filepath.Join(root, "node_modules", e.Name()))
				continue
			}
			members, err := os.ReadDir(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range members {
				link(filepath.Join(dir, e.Name(), m.Name()), filepath.Join(root, "node_modules", e.Name(), m.Name()))
			}
		}
	}
	for _, name := range []string{"system", "material"} {
		real, err := filepath.EvalSymlinks(filepath.Join(modules, "@mui", name))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "node_modules", "@mui", name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.CopyFS(target, os.DirFS(real)); err != nil {
			t.Fatal(err)
		}
		// The packages next to the real folder are its own dependencies.
		linkAll(filepath.Dir(filepath.Dir(real)))
	}
	linkAll(modules)
	if change != nil {
		for _, file := range []string{"memoTheme.mjs", "memoTheme.js"} {
			path := filepath.Join(root, "node_modules", "@mui", "system", file)
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(change(string(source))), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func memoThemeRender(t *testing.T, root string) ([]memoThemeCase, string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		"memo-theme.tsx": memoThemePage,
		"memo.cjs":       "module.exports = require('./node_modules/@mui/system/memoTheme.js').default\n",
	})
	code, printed := buildPrinting(t, root, "memo-theme.tsx")
	var cases []memoThemeCase
	if err := json.Unmarshal([]byte(evalResult(t, code)), &cases); err != nil {
		t.Fatal(err)
	}
	return cases, printed
}

// MUI's ThemeProvider spreads the theme into a new object on every render, and memoTheme of
// @mui/system keeps the styles it made only for the same object, so on the server it makes them
// again on every render. The tifa plugin makes memoTheme, as an ES module and as a CommonJS file,
// take a theme of the same content as the first theme of that content it saw. memoTheme gives and
// the pages render what they do without the change, and a memoTheme of another text is left as it
// is, with a warning.
func TestSSRMemoThemeTakesThemesOfTheSameContentAsOne(t *testing.T) {
	changed, printed := memoThemeRender(t, withMUI(t, nil))
	if strings.Contains(printed, notRecognized) {
		t.Errorf("the build prints:\n%s", printed)
	}
	// The style functions run fewer times where the plugin changed memoTheme: the counts below show
	// that it changed both files and only the ones it knows.
	plain, printed := memoThemeRender(t, withMUI(t, func(source string) string {
		return strings.Replace(source, "let lastTheme;", "let lastTheme; /* changed */", 1)
	}))
	for _, file := range []string{"memoTheme.mjs", "memoTheme.js"} {
		if !strings.Contains(printed, notRecognized+"@mui/system 9.5.0: ") || !strings.Contains(printed, file+" has no memoTheme the tifa plugin knows") {
			t.Errorf("an unknown %s: the build prints:\n%s", file, printed)
		}
	}
	// How many times the style function runs without the change and with it. A theme of the same
	// content is taken as one, and every other theme makes the styles again.
	runs := map[string][2]int{
		"the same object":                   {1, 1},
		"a copy":                            {2, 1},
		"copies on three renders":           {3, 1},
		"a changed value":                   {2, 2},
		"a key added":                       {2, 2},
		"a key removed":                     {2, 2},
		"another prototype":                 {2, 2},
		"no prototype":                      {2, 2},
		"keys in another order":             {2, 2},
		"a hidden key":                      {2, 2},
		"a symbol key":                      {2, 1},
		"a getter":                          {2, 2},
		"back and forth":                    {4, 4},
		"no theme":                          {1, 1},
		"a style function that throws":      {2, 2},
		"a style function that throws once": {3, 2},
		"seven other themes":                {2, 1},
		"eight other themes":                {2, 2},
		"one ThemeProvider":                 {2, 1},
		"nested ThemeProviders":             {6, 5},
		"CSS variables":                     {2, 2},
	}
	if len(plain) != len(changed) || len(changed) != 2*18+3 {
		t.Fatalf("%d cases, and %d without the change", len(changed), len(plain))
	}
	for i, p := range plain {
		c := changed[i]
		if c.Name != p.Name {
			t.Fatalf("case %d is %s, want %s", i, c.Name, p.Name)
		}
		if strings.Join(c.Out, "\n") != strings.Join(p.Out, "\n") {
			t.Errorf("%s: gives\n%s\nwant\n%s", c.Name, strings.Join(c.Out, "\n"), strings.Join(p.Out, "\n"))
		}
		name := c.Name
		if _, after, ok := strings.Cut(name, ": "); ok {
			name = after
		}
		want, ok := runs[name]
		if !ok {
			t.Fatalf("no runs for %s", c.Name)
		}
		if p.Runs != want[0] || c.Runs != want[1] {
			t.Errorf("%s: the style function runs %d times, and %d without the change, want %d and %d", c.Name, c.Runs, p.Runs, want[1], want[0])
		}
	}
}

// memoTheme of @mui/system has had one text from 6.1 to 9.5, in an ES module and a CommonJS file
// that differ in the call of preprocessStyles. Up to 7 the ES module was esm/memoTheme.js (and
// modern/memoTheme.js in 6 and 7.0), and from 9 it is memoTheme.mjs. The plugin knows the files by
// their path and changes both forms of the text to code that parses. A text with any other change
// is not known.
func TestPluginKnowsMemoThemeOfEveryLayout(t *testing.T) {
	system := filepath.Join(testModules(t), "@mui", "system")
	esm, err := os.ReadFile(filepath.Join(system, "memoTheme.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	cjs, err := os.ReadFile(filepath.Join(system, "memoTheme.js"))
	if err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{
		"9 esm":   string(esm),
		"9 cjs":   string(cjs),
		"6 esm":   strings.Replace(string(esm), `"./preprocessStyles.mjs"`, `"./preprocessStyles.js"`, 1),
		"changed": strings.Replace(string(esm), "lastValue = value;", "lastValue = value; ", 1),
		"no arg":  strings.Replace(string(esm), "theme: undefined", "theme: null", 1),
	}
	input, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	script := `
const { memoThemeByContent, memoThemeModule } = await import(process.argv[1])
const { parseAst } = await import('vite')
const sources = JSON.parse(process.argv[2])
const out = {}
for (const [name, source] of Object.entries(sources)) {
  const code = memoThemeByContent(source)
  let parses = false
  try { parses = code !== undefined && !!parseAst(code, { lang: 'js' }) } catch {}
  out[name] = { changed: code !== undefined, parses }
}
const paths = ['/n/@mui/system/memoTheme.mjs', '/n/@mui/system/memoTheme.js', '/n/@mui/system/esm/memoTheme.js',
  '/n/@mui/system/modern/memoTheme.js', 'C:\\n\\@mui\\system\\esm\\memoTheme.js', '/n/@mui/material/utils/memoTheme.mjs',
  '/n/@mui/system/memoTheme.d.ts', '/n/@mui/system/legacy/memoTheme.js']
out.paths = paths.map((p) => memoThemeModule.test(p))
console.log(JSON.stringify(out))
`
	cmd := exec.Command("node", "--input-type=module", "-e", script, plugin, string(input))
	cmd.Dir = testdata(t)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("memoThemeByContent: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	if string(got["paths"]) != "[true,true,true,true,true,false,false,false]" {
		t.Errorf("the paths the plugin knows: %s", got["paths"])
	}
	for name := range sources {
		var r struct {
			Changed, Parses bool
		}
		if err := json.Unmarshal(got[name], &r); err != nil {
			t.Fatal(err)
		}
		known := !strings.HasPrefix(name, "changed") && name != "no arg"
		if r.Changed != known || r.Parses != known {
			t.Errorf("%s: changed %v, parses %v, want %v", name, r.Changed, r.Parses, known)
		}
	}
}
