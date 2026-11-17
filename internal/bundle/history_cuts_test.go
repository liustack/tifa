package bundle

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// historyPage writes hrefs with the history of a router and with a copy of the function of
// @tanstack/history that turns a protocol-relative URL into a path, and counts the regular
// expressions the history runs for ordinary paths.
const historyPage = `import { createMemoryHistory, createRootRoute, createRouter } from '@tanstack/react-router'

const router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory({ initialEntries: ['/'] }), isServer: true })

const prefix = /^[\x00-\x20]*(?:[\\/][\t\n\r]*){2,}/
function reference(href: string) {
  if (/[\x00-\x1f\x7f]/.test(href)) href = href.replace(/[\x00-\x1f\x7f]/g, (character) => '\t\n\r'.includes(character) ? '' : encodeURIComponent(character))
  const match = prefix.exec(href)
  return match ? '/' + href.slice(match[0].length) : href
}

const hrefs = ['', '/', '/a', '/store/products/x?a=b#c', 'a', 'a/b', '//', '//evil.example', '///x', '/\\evil.example', '\\\\evil', '\\/x', '/\\', ' /x', ' //x', '\t//x', '/\t/x', '/\n\\x', '\x00//x', '\x1f/x', '/\t', '/ /x', '/a//b', 'https://x.test/a', '?q=1', '#h', 'é/x', '/é', '%2F%2Fx', '/ \t/x', '\x7f//x', '\x7f/x', '/\r\r/x']

;(async () => {
  const wrong: string[] = []
  for (const href of hrefs) {
    const got = router.history.createHref(href)
    const want = reference(href)
    if (got !== want) wrong.push(JSON.stringify(href) + ' ' + JSON.stringify(got) + ' ' + JSON.stringify(want))
  }
  let execs = 0
  const exec = RegExp.prototype.exec
  RegExp.prototype.exec = function (this: RegExp, text: string) {
    execs++
    return exec.call(this, text)
  }
  for (const href of ['/store', '/store/products/a', '/a?q=1', '/zh-CN/store']) router.history.createHref(href)
  RegExp.prototype.exec = exec
  ;(globalThis as any).result = JSON.stringify({ wrong, execs, n: hrefs.length })
})().catch((e) => {
  ;(globalThis as any).result = JSON.stringify({ wrong: [String(e) + ' ' + (e && e.stack)] })
})
`

// The history writes every href as before, and runs one regular expression for each ordinary path (its
// test for control characters) where it ran two.
func TestSSRHistoryNormalizesTheSameHrefsWithoutExec(t *testing.T) {
	code, printed := i18nextApp(t, prefixLinksRoot(t), historyPage, `{ target: ["es2022"] }`)
	if strings.Contains(printed, notRecognized) {
		t.Errorf("the build prints:\n%s", printed)
	}
	var r struct {
		Wrong []string `json:"wrong"`
		Execs int      `json:"execs"`
		N     int      `json:"n"`
	}
	raw := evalWithIntl(t, code)
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if len(r.Wrong) > 0 {
		t.Errorf("%d of %d hrefs are written another way, the first: %s", len(r.Wrong), r.N, r.Wrong[0])
	}
	if r.Execs > 4 {
		t.Errorf("%d calls of exec for four ordinary paths, want 4 or fewer (the one of the test for control characters)", r.Execs)
	}
}

// historyPlugin changes normalizeProtocolRelative only in the form it knows, and warns for a file
// of @tanstack/history that has it in another form.
func TestHistoryPluginChangesOnlyTheKnownForm(t *testing.T) {
	modules, err := filepath.Abs(filepath.Join("..", "..", "test", "apps", "pages", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	dir, _, err := findPackage(filepath.Join(modules, ".."), "@tanstack/react-router")
	if err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}
	history, _, err := findPackage(dir, "@tanstack/history")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(history, "dist", "esm", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	known := string(data)
	const body = `return match ? "/" + url.slice(match[0].length) : url;`
	if strings.Count(known, body) != 1 {
		t.Fatalf("history has %d of %q, the test needs one", strings.Count(known, body), body)
	}
	root := t.TempDir()
	files := map[string]string{"known": known, "reflowed": strings.ReplaceAll(known, "\t", "  "), "moved": strings.Replace(known, body, `return match ? url.slice(match[0].length) : url;`, 1),
		// Only the pattern the pre-check was written for changes, or its flags.
		"pattern wider": strings.Replace(known, `[\x00-\x20]*`, `[\x00-\x20\u00a0]*`, 1),
		"pattern flags": strings.Replace(known, `{2,}/;`, `{2,}/u;`, 1)}
	paths := map[string]string{}
	for name, source := range files {
		d := filepath.Join(root, name, "node_modules", "@tanstack", "history")
		writeTree(t, d, map[string]string{"package.json": `{ "name": "@tanstack/history", "version": "7.0.0" }`, "dist/esm/index.js": source})
		paths[name] = filepath.Join(d, "dist", "esm", "index.js")
	}
	pathsJSON, err := json.Marshal(paths)
	if err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
const { historyPlugin } = await import(process.argv[1])
const paths = JSON.parse(await (await import('node:stream/consumers')).text(process.stdin))
const out = {}
for (const [name, file] of Object.entries(paths)) {
  const plugin = historyPlugin()
  const warnings = []
  plugin.buildStart()
  const code = plugin.load(file)
  plugin.buildEnd.call({ warn: (message) => warnings.push(message) })
  out[name] = { changed: code !== null && code.includes('const first = url.charCodeAt(0)'), warnings }
}
process.stdout.write(JSON.stringify(out))
`
	cmd := nodeWithInput(testdata(t), script, pathsJSON, plugin)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	var got map[string]struct {
		Changed  bool     `json:"changed"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	for name, want := range map[string]bool{"known": true, "reflowed": true, "moved": false, "pattern wider": false, "pattern flags": false} {
		g := got[name]
		if g.Changed != want {
			t.Errorf("%s: changed %v, want %v", name, g.Changed, want)
		}
		if want == (len(g.Warnings) > 0) || (!want && !strings.HasPrefix(g.Warnings[0], notRecognized+"@tanstack/history 7.0.0")) {
			t.Errorf("%s: warnings %q", name, g.Warnings)
		}
	}
}
