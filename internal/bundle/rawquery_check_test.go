package bundle

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// buildWarnings builds the SSR bundle of an app made of files, whose .tifa/server.ts is the entry,
// with @tanstack/react-router standing for a module of three hooks that return what they are given,
// and query as the table of .tifa/vite.js. It returns what the build printed.
func buildWarnings(t *testing.T, files map[string]string, query string) string {
	t.Helper()
	root := copyTestdata(t, "ssr", true)
	tree := map[string]string{
		"package.json": `{ "type": "module" }` + "\n",
		"vite.config.ts": "import tifa from './.tifa/vite.js'\n" +
			"export default { plugins: [tifa()], resolve: { alias: { '@tanstack/react-router': '/router.ts' } } }\n",
		"router.ts": "export const useSearch = (o?: any): any => o\nexport const useLocation = (o?: any): any => o\n" +
			"export const useRouterState = (o?: any): any => o\nexport const Link = 1\n",
		".tifa/vite/plugin.js": js.VitePlugin,
		".tifa/vite.js":        "import { tifa } from './vite/plugin.js'\nconst query = " + query + "\nexport default () => tifa({ target: ['es2022'], query })\n",
		".tifa/client.ts":      "export const client = 1\n",
	}
	for name, text := range files {
		tree[name] = text
	}
	writeTree(t, root, tree)
	var output strings.Builder
	n := NewNode(root, &output)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), true, nil); err != nil {
		t.Fatalf("the build failed: %v\n%s", err, output.String())
	}
	return output.String()
}

// A read of the query in a module that is no page or Layout gets a warning with its place, since it
// renders under pages that read other parameters. The build goes on.
func TestBuildWarnsOfRawQueryReads(t *testing.T) {
	page := `import { useLocation, useRouterState, useSearch as search } from '@tanstack/react-router'

export function A() { return search({ strict: false }).utm_source }
export function B() { return useLocation({ select: (l: any) => l.searchStr }) }
export function C() { const location = useLocation(); return location.href }
export function D() { return useRouterState({ select: (s: any) => s.location.search }) }
export function E() { return useRouterState({ select: ({ location }: any) => location.search }) }
export function F() { const { search: q } = useLocation(); return q }
export function G() { return useLocation().search.q }
export function H() { const state = useRouterState(); return state.resolvedLocation.search }
;(globalThis as any).result = [A, B, C, D, E, F, G, H]
`
	out := buildWarnings(t, map[string]string{".tifa/server.ts": page}, "{}")
	for _, want := range []string{
		`.tifa/server.ts:3:30: the query parameter "utm_source" is read while rendering`,
		".tifa/server.ts:4:", ".tifa/server.ts:5:", ".tifa/server.ts:6:", ".tifa/server.ts:7:",
		".tifa/server.ts:8:", ".tifa/server.ts:9:", ".tifa/server.ts:10:",
		"no page.tsx or layout.tsx", "read it in the Load", "does not make a read safe",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the warnings lack %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "is read while rendering"); n != 8 {
		t.Errorf("%d warnings, want 8:\n%s", n, out)
	}
}

// In a page and a Layout, a parameter read by its name is checked against those the server renders
// it with, and so is a member read from the useSearch of a route. A read in an effect, a name of an
// inner function and a hook of the app's own are left alone.
func TestBuildChecksQueryReadsByTheirNames(t *testing.T) {
	files := map[string]string{
		".tifa/server.ts": "import { Warns, Fine } from '../app/news/page'\nimport { Layout } from '../app/news/layout'\nimport { List } from '../app/news/list'\n" +
			"import { Other } from '../app/plain/page'\n;(globalThis as any).result = [Warns, Fine, Layout, List, Other]\n",
		"app/news/route.gen.ts": "export const page = { useSearch: (o?: any): any => o ?? {} }\nexport const layout = page\n",
		"app/news/page.tsx": `import * as R from '@tanstack/react-router'
import { useLocation, useRouterState, useSearch } from '@tanstack/react-router'
import React, { useEffect, useMemo } from 'react'
import { page } from './route.gen'

export function Warns() {
  const a = useLocation().search.utm
  const b = useRouterState({ select: (s: any) => s.location.search })
  const c = page.useSearch().utm
  const s = page.useSearch(); const d = s['utm']
  const e = R.useLocation().search.utm
  const f = (useLocation() as any)['search'].utm
  const g = useLocation().searchStr
  const l = useLocation(); const h = useMemo(() => l.search.utm, [l])
  const i = useSearch({ strict: false }); const j = i[String(a)]
  return [a, b, c, d, e, f, g, h, j]
}

export function Fine() {
  const a = useRouterState({ select: (s: any) => s.location.search.q })
  const b = useLocation().search['page']
  const c = page.useSearch().q
  const d = page.useSearch()
  const { q } = page.useSearch()
  const { search: { page: p } } = useLocation()
  const l = useLocation()
  useEffect(() => { console.log(l.search.utm, l.href) }, [l])
  React.useLayoutEffect(() => { console.log(l.searchStr) })
  function inner(location: any) { return location.search }
  const t = useSearch({ strict: false }).q
  return [a, b, c, d, q, p, inner, t]
}
`,
		"app/news/layout.tsx": "import { useLocation } from '@tanstack/react-router'\nexport function Layout() { return [useLocation().search.page, useLocation().search.q] }\n",
		"app/news/list.tsx":   "import { useLocation } from '@tanstack/react-router'\nexport function List() { return useLocation().search.q }\n",
		"app/plain/page.tsx":  "import { layout } from '../news/route.gen'\nexport function Other() { return [layout.useSearch().page, layout.useSearch().q] }\n",
	}
	out := buildWarnings(t, files, `{"app/news":{"layout":["page"],"page":["page","q"]},"app/plain":{"page":[]}}`)
	for line := 7; line <= 15; line++ {
		if want := "app/news/page.tsx:" + strconv.Itoa(line) + ":"; !strings.Contains(out, want) {
			t.Errorf("no warning at %s:\n%s", want, out)
		}
	}
	for _, want := range []string{
		`app/news/page.tsx:7:13: the query parameter "utm" is read while rendering, and the server renders this code with the query parameters page, q only`,
		"app/news/page.tsx:8:50: location.search is read",
		"app/news/page.tsx:13:13: location.searchStr is read",
		"app/news/page.tsx:15:53: useSearch({ strict: false })[...] is read",
		`app/news/layout.tsx:2:63: the query parameter "q" is read while rendering, and the server renders this code with the query parameters page only`,
		`app/news/list.tsx:2:`,
		`app/plain/page.tsx:2:60: the query parameter "q"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the warnings lack %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "is read while rendering"); n != 12 {
		t.Errorf("%d warnings, want 12:\n%s", n, out)
	}
}


// What is tied to the route, or reads no query, gets no warning: the useSearch of a route, the path
// of the location, a hook of the app's own and the location of the window.
func TestBuildLetsRouteQueryReadsThrough(t *testing.T) {
	page := `import { useLocation, useRouterState, useSearch } from '@tanstack/react-router'

const route = { useSearch: (o?: any): any => o }
export function A() { return route.useSearch().page }
export function B() { return useSearch({ from: '/news' }) }
export function C() { const location = useLocation(); return location.pathname }
export function D() { return useRouterState({ select: (s: any) => s.location.pathname + s.isLoading }) }
export function E() { return globalThis.location?.search }
export function F() { const location = useLocation(); return location.pathname }
export function G() { const location = { search: '' }; return location.search }
;(globalThis as any).result = [A, B, C, D, E, F, G]
`
	if out := buildWarnings(t, map[string]string{".tifa/server.ts": page}, "{}"); strings.Contains(out, "is read while rendering") {
		t.Errorf("a warning for a read tied to the route:\n%s", out)
	}
}
