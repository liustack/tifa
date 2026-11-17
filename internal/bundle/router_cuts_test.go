package bundle

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

// cutsPage builds links with a router whose routes count what buildLocation reads of them, and
// compares each link with the one a router that was never asked before builds.
const cutsPage = `import { createMemoryHistory, createRootRoute, createRoute, createRouter } from '@tanstack/react-router'

let searchReads = 0
let paramReads = 0
let middlewareCalls = 0

const root = createRootRoute()
const store = createRoute({ getParentRoute: () => root, path: '/store' })
Object.defineProperty(store.options, 'search', { get: () => (searchReads++, undefined), enumerable: true, configurable: true })
const item = createRoute({ getParentRoute: () => root, path: '/store/$id' })
const found = createRoute({
  getParentRoute: () => root,
  path: '/found',
  search: { middlewares: [({ search, next }: any) => (middlewareCalls++, next({ ...search, m: 1 }))] },
})
const tree = root.addChildren([store, item, found])

function make() {
  return createRouter({ routeTree: tree, history: createMemoryHistory({ initialEntries: ['/store'] }), isServer: true })
}

const countedParams = () => ({ get x() { paramReads++; return 1 } })

;(async () => {
  const out: any = {}
  const router = make()
  searchReads = 0
  const a = router.buildLocation({ to: '/store' })
  const b = router.buildLocation({ to: '/store' })
  out.searchReads = searchReads
  out.sameStatic = JSON.stringify(a) === JSON.stringify(b) && a.publicHref === '/store'
  // A link that asks for the validators of the routes is not answered from the others.
  searchReads = 0
  router.buildLocation({ to: '/store', _includeValidateSearch: true } as any)
  router.buildLocation({ to: '/store', _includeValidateSearch: true } as any)
  out.validateReads = searchReads
  // The middlewares of a route run for every link, and the links are the same as ever.
  middlewareCalls = 0
  const m1 = router.buildLocation({ to: '/found', search: { a: 'b' } })
  const m2 = router.buildLocation({ to: '/found', search: { a: 'b' } })
  out.middlewareCalls = middlewareCalls
  out.middlewareLinks = [m1.publicHref, m2.publicHref]
  // Params of a link with no template in its path are not looked at, a function of them is called.
  paramReads = 0
  const p = router.buildLocation({ to: '/store', params: countedParams() } as any)
  out.paramReads = paramReads
  out.paramLink = p.publicHref
  let called = 0
  router.buildLocation({ to: '/store', params: () => (called++, {}) } as any)
  out.functionCalls = called
  // A template takes its params, from the link or from the location.
  out.templated = router.buildLocation({ to: '/store/$id', params: { id: 'a' } }).publicHref
  out.inherited = router.buildLocation({ to: '/store/$id', _fromLocation: router.parseLocation({ pathname: '/store/z', search: {}, hash: '', href: '/store/z', state: {} } as any) } as any).publicHref
  ;(globalThis as any).result = JSON.stringify(out)
})().catch((e) => {
  ;(globalThis as any).result = JSON.stringify({ error: String(e) + ' ' + (e && e.stack) })
})
`

// buildLocation asks a route for its search middlewares once for all the links to it, and does not
// work out the params of a link whose path has no template. and does not
// work out the params of a link whose path has no template.
func TestSSRBuildLocationDoesNotRepeatWork(t *testing.T) {
	code, printed := i18nextApp(t, prefixLinksRoot(t), cutsPage, `{ target: ["es2022"] }`)
	if strings.Contains(printed, notRecognized) {
		t.Errorf("the build prints:\n%s", printed)
	}
	var r struct {
		Error           string   `json:"error"`
		SearchReads     int      `json:"searchReads"`
		SameStatic      bool     `json:"sameStatic"`
		ValidateReads   int      `json:"validateReads"`
		MiddlewareCalls int      `json:"middlewareCalls"`
		MiddlewareLinks []string `json:"middlewareLinks"`
		ParamReads      int      `json:"paramReads"`
		ParamLink       string   `json:"paramLink"`
		FunctionCalls   int      `json:"functionCalls"`
		Templated       string   `json:"templated"`
		Inherited       string   `json:"inherited"`
	}
	raw := evalWithIntl(t, code)
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	if !r.SameStatic || r.ParamLink != "/store" || r.Templated != "/store/a" || r.Inherited != "/store/z" {
		t.Errorf("the links changed: %s", raw)
	}
	if r.MiddlewareCalls != 2 || r.MiddlewareLinks[0] != "/found?a=b" || r.MiddlewareLinks[1] != r.MiddlewareLinks[0] {
		t.Errorf("a middleware does not run for each link: %s", raw)
	}
	if r.FunctionCalls != 1 {
		t.Errorf("a function of params is called %d times, want 1", r.FunctionCalls)
	}
	if r.SearchReads != 1 {
		t.Errorf("the search of a route is read %d times for two links, want 1", r.SearchReads)
	}
	if r.ValidateReads != 2 {
		t.Errorf("the search of a route is read %d times for two links that ask for the validators, want 2", r.ValidateReads)
	}
	if r.ParamReads != 0 {
		t.Errorf("the params of a link without a template are read %d times, want 0", r.ParamReads)
	}
}
