package bundle

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// buildWarnings builds page as the SSR bundle of an app, with @tanstack/react-router standing for a
// module of three hooks that return what they are given, and returns what the build printed.
func buildWarnings(t *testing.T, page string) string {
	t.Helper()
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{
		"package.json": `{ "type": "module" }` + "\n",
		"vite.config.ts": "import tifa from './.tifa/vite.js'\n" +
			"export default { plugins: [tifa()], resolve: { alias: { '@tanstack/react-router': '/router.ts' } } }\n",
		"router.ts": "export const useSearch = (o?: any): any => o\nexport const useLocation = (o?: any): any => o\n" +
			"export const useRouterState = (o?: any): any => o\nexport const Link = 1\n",
		".tifa/vite/plugin.js": js.VitePlugin,
		".tifa/vite.js":        "import { tifa } from './vite/plugin.js'\nexport default () => tifa({ target: ['es2022'] })\n",
		".tifa/client.ts":      "export const client = 1\n",
		".tifa/server.ts":      page,
	})
	var output strings.Builder
	n := NewNode(root, &output)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), true, nil); err != nil {
		t.Fatalf("the build failed: %v\n%s", err, output.String())
	}
	return output.String()
}

// A read of the query that is not tied to the parameters of a route gets a warning with its place,
// since the server renders with the parameters the Loads read only. The build goes on.
func TestBuildWarnsOfRawQueryReads(t *testing.T) {
	page := `import { useLocation, useRouterState, useSearch as search } from '@tanstack/react-router'

export function A() { return search({ strict: false }).utm_source }
export function B() { return useLocation({ select: (l: any) => l.searchStr }) }
export function C() { const location = useLocation(); return location.href }
export function D() { return useRouterState({ select: (s: any) => s.location.search }) }
export function E() { return useRouterState({ select: ({ location }: any) => location.search }) }
export function F() { const { search: q } = useLocation(); return q }
export function G() { return useLocation().search }
export function H() { const state = useRouterState(); return state.resolvedLocation.search }
;(globalThis as any).result = [A, B, C, D, E, F, G, H]
`
	out := buildWarnings(t, page)
	for _, want := range []string{
		".tifa/server.ts:3:", "useSearch({ strict: false })",
		".tifa/server.ts:4:", ".tifa/server.ts:5:", ".tifa/server.ts:6:", ".tifa/server.ts:7:",
		".tifa/server.ts:8:", ".tifa/server.ts:9:", ".tifa/server.ts:10:",
		"must be one the Load of the page or of a Layout reads", "read it in the Load",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the warnings lack %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "reads the query of the URL while rendering"); n != 8 {
		t.Errorf("%d warnings, want 8:\n%s", n, out)
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
	if out := buildWarnings(t, page); strings.Contains(out, "reads the query of the URL") {
		t.Errorf("a warning for a read tied to the route:\n%s", out)
	}
}
