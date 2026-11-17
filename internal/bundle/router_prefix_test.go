package bundle

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// prefixLinksPage builds the links of a site with two languages with two routers that differ in
// one flag only. The rewrite is the shape of Tifa's PrefixRewrite (it changes url.pathname only,
// prefixes the paths of /store for zh and leaves the rest). The flag pathOnly lets router-core's
// buildLocation skip the URL for a plain path. The page counts the URLs each router builds.
const prefixLinksPage = `import { createMemoryHistory, createRootRoute, createRoute, createRouter } from '@tanstack/react-router'

const root = createRootRoute()
const store = createRoute({ getParentRoute: () => root, path: '/store' })
const item = createRoute({ getParentRoute: () => root, path: '/store/$id' })
const projects = createRoute({ getParentRoute: () => root, path: '/projects' })
const tree = root.addChildren([store, item, projects])

let built = 0
const NativeURL = URL
;(globalThis as any).URL = class extends NativeURL {
  constructor(...args: ConstructorParameters<typeof URL>) {
    super(...args)
    built++
  }
}

function make(language: string, pathOnly: boolean) {
  const rewrite: any = {
    output: ({ url }: { url: URL }) => {
      if (language !== 'en' && url.pathname.startsWith('/store')) url.pathname = '/' + language + url.pathname
      return url
    },
  }
  if (pathOnly) rewrite.pathOnly = true
  return createRouter({ routeTree: tree, history: createMemoryHistory({ initialEntries: ['/store'] }), rewrite })
}

const cases: Array<[string, any]> = [
  ['plain', { to: '/store' }],
  ['param', { to: '/store/$id', params: { id: 'abc-1' } }],
  ['search', { to: '/store', search: { q: 'tea', page: 2 } }],
  ['hash', { to: '/store', hash: 'top' }],
  ['no languages', { to: '/projects' }],
  ['non-ascii param', { to: '/store/$id', params: { id: 'café' } }],
  ['apostrophe in search', { to: '/store', search: { q: "it's" } }],
  ['dot in name', { to: '/store/$id', params: { id: 'a.b' } }],
  ['dot file', { to: '/store/$id', params: { id: 'x.tar.gz' } }],
  ['dot segments', { to: '/store/$id', params: { id: '%2e%2e' } }],
  ['dot segment', { to: '/store/$id', params: { id: '..' } }],
  ['space', { to: '/store/$id', params: { id: 'a b' } }],
  ['space in search', { to: '/store', search: { q: 'a b' } }],
  ['space in hash', { to: '/store', hash: 'a b' }],
  ['apostrophe in hash', { to: '/store', hash: "it's" }],
  ['percent', { to: '/store/$id', params: { id: '100%25' } }],
  ['slashes', { to: '/store/$id', params: { id: 'a//b' } }],
]

;(async () => {
  const out: any[] = []
  for (const language of ['en', 'zh']) {
    const plain = make(language, false)
    const fast = make(language, true)
    for (const [name, opts] of cases) {
      built = 0
      const a = plain.buildLocation(opts)
      const slowURLs = built
      built = 0
      const b = fast.buildLocation(opts)
      out.push({ name: language + ' ' + name, same: JSON.stringify(a) === JSON.stringify(b), a: a.publicHref, b: b.publicHref, slowURLs, fastURLs: built })
    }
  }
  ;(globalThis as any).result = JSON.stringify(out)
})().catch((e) => {
  ;(globalThis as any).result = JSON.stringify([{ name: String(e) + " " + (e && e.stack) }])
})
`

// prefixLinksRoot is an app whose node_modules are those of test/apps/pages, which install the
// tested TanStack Router.
func prefixLinksRoot(t *testing.T) string {
	t.Helper()
	modules, err := filepath.Abs(filepath.Join("..", "..", "test", "apps", "pages", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modules, "@tanstack", "react-router")); err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}
	root := t.TempDir()
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return root
}

// A rewrite marked pathOnly makes router-core build a link without a URL when the path is plain,
// and the link is the one it makes with the URL. Every path the check cannot be sure of keeps the
// URL.
func TestSSRPathOnlyRewriteBuildsTheSameLinksWithoutURL(t *testing.T) {
	code, printed := i18nextApp(t, prefixLinksRoot(t), prefixLinksPage, `{ target: ["es2022"] }`)
	if strings.Contains(printed, notRecognized) {
		t.Errorf("the build prints:\n%s", printed)
	}
	var results []struct {
		Name     string `json:"name"`
		Same     bool   `json:"same"`
		A        string `json:"a"`
		B        string `json:"b"`
		SlowURLs int    `json:"slowURLs"`
		FastURLs int    `json:"fastURLs"`
	}
	if err := json.Unmarshal([]byte(evalWithIntl(t, code)), &results); err != nil {
		t.Fatal(err)
	}
	// The cases the fast path takes. The others must keep the URL, and the link stays the same.
	fast := map[string]bool{"plain": true, "param": true, "search": true, "hash": true, "no languages": true, "percent": false,
		// The router writes a quote or a space of a search value as %27 or +, so the path is plain.
		"apostrophe in search": true, "space in search": true,
		// A dot in a name is no dot segment: the plain checks decide it.
		"dot in name": true, "dot file": true}
	ran := 0
	for _, r := range results {
		if !r.Same {
			t.Errorf("%s: %q with the flag, %q without it", r.Name, r.B, r.A)
		}
		_, name, _ := strings.Cut(r.Name, " ")
		if fast[name] {
			ran++
			if r.FastURLs != 0 || r.SlowURLs == 0 {
				t.Errorf("%s: %d URLs with the flag, %d without it, want 0 and more", r.Name, r.FastURLs, r.SlowURLs)
			}
		} else if r.FastURLs != r.SlowURLs {
			t.Errorf("%s: %d URLs with the flag, %d without it, want the same", r.Name, r.FastURLs, r.SlowURLs)
		}
	}
	if ran == 0 {
		t.Error("no case took the fast path")
	}
}

// A router-core whose buildLocation is not in the form the plugin knows makes the build print the
// text, so an upgrade that moves the code is found, and every link keeps its URL.
func TestSSRRouterCoreInAnotherFormPrintsTheText(t *testing.T) {
	root := prefixLinksRoot(t)
	modules := filepath.Join(root, "node_modules")
	real, _ := os.Readlink(modules)
	os.Remove(modules)
	entries, err := os.ReadDir(real)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "@tanstack" || strings.HasPrefix(e.Name(), ".bin") {
			continue
		}
		symlink(t, filepath.Join(real, e.Name()), filepath.Join(modules, e.Name()))
	}
	writeTree(t, root, map[string]string{
		"node_modules/@tanstack/router-core/package.json":       `{ "name": "@tanstack/router-core", "version": "9.9.9", "type": "module", "main": "dist/esm/router.js" }`,
		"node_modules/@tanstack/router-core/dist/esm/router.js": "export const x = 1\nif (this.rewrite) {\n  const url = new URL(path, this.origin);\n}\n",
	})
	code, printed := i18nextApp(t, root, "import { x } from '@tanstack/router-core'\n;(globalThis as any).result = x\n", `{ target: ["es2022"] }`)
	if !strings.Contains(printed, notRecognized+"@tanstack/router-core 9.9.9") {
		t.Errorf("the build prints:\n%s", printed)
	}
	if strings.Contains(code, "tifaPathOnly") {
		t.Error("the plugin changed a file it does not know")
	}
}

// routerCoreSource is the dist/esm/router.js of the router-core test/apps/pages installs.
func routerCoreSource(t *testing.T) string {
	t.Helper()
	modules, err := filepath.Abs(filepath.Join("..", "..", "test", "apps", "pages", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	dir, _, err := findPackage(filepath.Join(modules, ".."), "@tanstack/react-router")
	if err != nil {
		t.Skip("run pnpm install --frozen-lockfile in test/apps/pages first")
	}
	core, _, err := findPackage(dir, "@tanstack/router-core")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(core, "dist", "esm", "router.js"))
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

// routerCorePlugin, called as Vite calls it but without Vite, changes the buildLocation of
// router-core only when all of the code of its rewrite branch is the one it knows (whitespace
// aside), and warns once for each file of router-core that it does not change, also when another
// copy in the same build is the known form.
func TestRouterCorePluginChangesOnlyTheKnownForm(t *testing.T) {
	known := routerCoreSource(t)
	const tail = "normalizeProtocolRelative(getUrlPath(rewrittenUrl))"
	if strings.Count(known, tail) != 1 {
		t.Fatalf("router-core has %d of %q, the test needs one", strings.Count(known, tail), tail)
	}
	variants := map[string]string{
		"known":      known,
		"reflowed":   strings.ReplaceAll(known, "\t", "  "),
		"tail moved": strings.Replace(known, tail, "getUrlPath(rewrittenUrl)", 1),
		"head moved": strings.Replace(known, "const url = new URL(fullPath, this.origin);", "const url = new URL(fullPath, 'http://x');", 1),
	}
	cases := []struct {
		name  string
		files []string
		// changed says for each file whether the plugin changed it, warned for the files that it did not.
		changed []bool
	}{
		{"known", []string{"known"}, []bool{true}},
		{"reflowed", []string{"reflowed"}, []bool{true}},
		{"tail moved", []string{"tail moved"}, []bool{false}},
		{"head moved", []string{"head moved"}, []bool{false}},
		{"known then unknown", []string{"known", "tail moved"}, []bool{true, false}},
		{"unknown then known", []string{"tail moved", "known"}, []bool{false, true}},
		{"two unknown", []string{"tail moved", "head moved"}, []bool{false, false}},
	}
	root := t.TempDir()
	type input struct {
		Name  string   `json:"name"`
		Files []string `json:"files"`
	}
	var inputs []input
	for i, c := range cases {
		in := input{Name: c.name}
		for j, v := range c.files {
			dir := filepath.Join(root, "case"+strconv.Itoa(i), "copy"+strconv.Itoa(j), "node_modules", "@tanstack", "router-core")
			writeTree(t, dir, map[string]string{
				"package.json":       `{ "name": "@tanstack/router-core", "version": "7.` + strconv.Itoa(j) + `.0" }`,
				"dist/esm/router.js": variants[v],
			})
			in.Files = append(in.Files, filepath.Join(dir, "dist", "esm", "router.js"))
		}
		inputs = append(inputs, in)
	}
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(t.TempDir(), "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(js.VitePlugin), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
const { routerCorePlugin } = await import(process.argv[1])
const cases = JSON.parse(await (await import('node:stream/consumers')).text(process.stdin))
const out = cases.map(({ name, files }) => {
  const plugin = routerCorePlugin()
  const warnings = []
  plugin.buildStart()
  const changed = files.map((file) => {
    const code = plugin.load(file)
    return code !== null && code.includes('tifaPathOnly(fullPath, nextPathname)') && code.includes('function tifaPathOnly(')
  })
  plugin.buildEnd.call({ warn: (message) => warnings.push(message) })
  return { name, changed, warnings }
})
process.stdout.write(JSON.stringify(out))
`
	cmd := nodeWithInput(testdata(t), script, data, plugin)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	var got []struct {
		Name     string   `json:"name"`
		Changed  []bool   `json:"changed"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	for i, c := range cases {
		g := got[i]
		if !slices.Equal(g.Changed, c.changed) {
			t.Errorf("%s: changed %v, want %v", c.name, g.Changed, c.changed)
		}
		wantWarnings := 0
		for j, changed := range c.changed {
			if changed {
				continue
			}
			wantWarnings++
			// The warning names the copy it is about, by its version and its file.
			want := notRecognized + "@tanstack/router-core 7." + strconv.Itoa(j) + ".0: "
			if !slices.ContainsFunc(g.Warnings, func(w string) bool { return strings.HasPrefix(w, want) && strings.Contains(w, "copy"+strconv.Itoa(j)) }) {
				t.Errorf("%s: no warning that starts %q for copy %d in %q", c.name, want, j, g.Warnings)
			}
		}
		if len(g.Warnings) != wantWarnings {
			t.Errorf("%s: %d warnings, want %d: %q", c.name, len(g.Warnings), wantWarnings, g.Warnings)
		}
	}
}
