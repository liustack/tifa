// Run by TestGeneratedRouterBehaves in Node, against the route tree generated for this app.
import { writeFileSync } from 'node:fs'
import { createMemoryHistory, isRedirect } from '@tanstack/react-router'
import { hydrate } from '@tanstack/react-router/ssr/client'
import { htmlAttributes, routeTree } from './.tifa/routes'
import { createAppRouter, load } from './.tifa/runtime/router'
import { serve, type RenderResult } from './.tifa/runtime/server'
import { collector, RouteError, type InjectedResult, type RouteResult } from './.tifa/runtime/data'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}
const ok = (data: unknown): RouteResult => ({ status: 200, data })
const missing: RouteResult = { status: 404 }

serve(routeTree, htmlAttributes)

// inject writes results the way Go injects them into a render: the data of a 200 as JSON text,
// which goes to startRender as an argument of its own, and the result says its number. A result
// that has its json already keeps that text.
function inject(data: Record<string, RouteResult | InjectedResult>) {
  const out: Record<string, RenderResult> = {}
  const texts: string[] = []
  for (const [id, result] of Object.entries(data)) {
    if (result.status !== 200) {
      out[id] = result as RenderResult
      continue
    }
    const { data: value, json, ...rest } = result as RouteResult & InjectedResult
    out[id] = { ...rest, text: texts.length }
    texts.push(json ?? JSON.stringify(value))
  }
  return { data: out, texts }
}

// renderResult runs one whole-page render through the SSR entry, as the Go VM pool does.
async function renderResult(url: string, data: Record<string, RouteResult | InjectedResult>, preload?: string) {
  globalThis.renderResult = undefined
  const input = inject(data)
  globalThis.startRender(JSON.stringify({ url, buildId: 'b1', data: input.data, preload }), ...input.texts)
  for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++)
    await new Promise((resolve) => setTimeout(resolve, 0))
  const result = globalThis.renderResult ?? '{"error":"render did not finish"}'
  // A page comes as the HTML itself, anything else as JSON.
  return (result.startsWith('<') ? { html: result } : JSON.parse(result)) as {
    html?: string
    redirect?: { location: string; status: number }
    error?: string
  }
}

async function render(url: string, data: Record<string, RouteResult | InjectedResult>, preload?: string): Promise<string> {
  const result = await renderResult(url, data, preload)
  if (result.html === undefined) throw new Error(`render ${url}: ${JSON.stringify(result)}`)
  return result.html
}

function contains(html: string, ...parts: string[]) {
  for (const part of parts) assert(html.includes(part), `HTML lacks ${part}:\n${html}`)
}
function lacks(html: string, ...parts: string[]) {
  for (const part of parts) assert(!html.includes(part), `HTML has ${part}:\n${html}`)
}

// client loads url on a browser-side router whose source records each call.
async function client(url: string, answer: (url: string, id: string) => RouteResult = (_, id) => ok(id)) {
  const calls: Array<[string, string]> = []
  const router = createAppRouter(
    routeTree,
    async (u, id) => {
      calls.push([u, id])
      return answer(u, id)
    },
    { history: createMemoryHistory({ initialEntries: [url] }) },
  )
  await router.load()
  return { router, calls }
}

const appData = { 'app/layout': ok({ name: 'router' }) }

test('sibling Layouts load only their own data', async () => {
  const { calls } = await client('/b/three')
  const ids = calls.map(([, id]) => id).sort()
  assert(JSON.stringify(ids) === '["app/b/layout","app/layout"]', `loaded ${ids}`)
  contains(await render('/a/one', { ...appData, 'app/a/layout': ok('a') }), 'LAYOUT_A', 'PAGE_A_ONE')
  contains(await render('/b/four', { ...appData, 'app/b/layout': ok('b') }), 'LAYOUT_B', 'PAGE_B_FOUR')
})

test('query values stay the strings Go reads', async () => {
  const { router, calls } = await client('/q?q=1.0')
  assert(router.state.location.href === '/q?q=1.0', `href ${router.state.location.href}`)
  assert(calls.some(([u, id]) => id === 'app/q/route' && u === '/q?q=1.0'), `calls ${JSON.stringify(calls)}`)
  const search = router.state.location.search as Record<string, unknown>
  assert(search.q === '1.0', `search ${JSON.stringify(search)}`)
  // Two integers JSON reads as one number are two matches.
  const ids: string[] = []
  for (const href of ['/q?q=9007199254740992', '/q?q=9007199254740993']) {
    router.history.push(href)
    await router.load()
    ids.push(router.state.matches[router.state.matches.length - 1].id)
  }
  assert(ids[0] !== ids[1], `one match for two values: ${ids}`)
  const repeated = await client('/q?q=a&q=b')
  const values = (repeated.router.state.location.search as Record<string, unknown>).q
  assert(JSON.stringify(values) === '["a","b"]', `repeated ${JSON.stringify(values)}`)
  // The query of a URL as browsers and forms write it comes back as it is.
  for (const href of ['/q?q=a&q=b', '/q?q=a+b', '/q?q=1,2:3/4', '/q?q=%E4%B8%AD&r=%26%3D%2B%25%23']) {
    const { router } = await client(href)
    assert(router.state.location.href === href, `${href} became ${router.state.location.href}`)
  }
  // A value Go would read differently unescaped, such as a semicolon, stays escaped on refetch.
  const semicolon = await client('/q?q=a%3Bb')
  semicolon.calls.length = 0
  await semicolon.router.invalidate()
  const refetch = semicolon.calls.find(([, id]) => id === 'app/q/route')
  assert(refetch?.[0] === '/q?q=a%3Bb', `refetched ${JSON.stringify(semicolon.calls)}`)
  // __proto__ is a parameter like any other: two values are two matches.
  const proto = await client('/q?__proto__=alice')
  const protoIds: string[] = []
  for (const href of ['/q?__proto__=alice', '/q?__proto__=bob']) {
    proto.router.history.push(href)
    await proto.router.load()
    const match = proto.router.state.matches[proto.router.state.matches.length - 1]
    protoIds.push(match.id + ' ' + JSON.stringify(match.loaderDeps))
  }
  assert(protoIds[0] !== protoIds[1], `one match for two values: ${protoIds}`)
  contains(await render('/q?q=1.0', { ...appData, 'app/q/route': ok('1.0') }), 'QUERY <!-- -->1.0')
})

test('paths match case-sensitively, as ServeMux does', async () => {
  contains(await render('/hello', appData), 'PAGE_LOWER')
  contains(await render('/HELLO', appData), 'APP_404')
})

test('literal segments match the URLs ServeMux matches', async () => {
  contains(await render('/e', appData), 'PAGE_ENCODED')
  // The router spells /%65 as /e and redirects there, Go then serves the redirect.
  const redirected = await renderResult('/%65', appData)
  assert(redirected.redirect?.location === '/e', `render /%65: ${JSON.stringify(redirected)}`)
})

test('the document around the React root is written as a string', async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': ok('one') })
  assert(
    html.startsWith('<!DOCTYPE html><html lang="xx" data-theme="a&quot;b" class="h-full"><head><meta charset="utf-8">'),
    `document start: ${html.slice(0, 200)}`,
  )
  contains(
    html,
    '<link rel="stylesheet" href="/assets/app.css">',
    '<script type="module" src="/assets/app.js"></script>',
    '<body><div id="tifa-root">',
    'data-tsr-stream-part',
    '</body></html>',
  )
  assert(html.split('<html').length === 2 && html.split('<body').length === 2, `one html and body: ${html}`)
  const root = html.slice(html.indexOf('<div id="tifa-root">'), html.indexOf('</div><script'))
  lacks(root, '<script type="module"', '<title>', 'data-tsr-stream-part')
})

test('the module preloads Go gives for the matched routes follow the asset tags', async () => {
  const data = { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': ok('one') }
  const preload = '<link rel="modulepreload" href="/assets/chunk-team.js"><link rel="modulepreload" href="/assets/chunk-one.js">'
  const html = await render('/teams/x/one', data, preload)
  contains(html, '<script type="module" src="/assets/app.js"></script>' + preload + '<!--tifa-head-->')
  // Without preloads the document is the one written before there were any, apart from the
  // times the router stamps its matches with.
  const stamps = (page: string) => page.replace(/\bu:\d+/g, 'u:0')
  const plain = await render('/teams/x/one', data)
  assert(stamps(html.replace(preload, '')) === stamps(plain), `the preloads change more than their place:\n${html}\n${plain}`)
})

test('the head holds the tags of the deepest route, escaped', async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': ok('one') })
  const head = html.slice(html.indexOf('<head>'), html.indexOf('</head>'))
  contains(
    head,
    '<meta name="viewport" content="width=device-width, initial-scale=1">',
    '<title>ONE_TITLE</title>',
    '<meta name="description" content="one">',
    '<link rel="icon" href="/icon.svg">',
  )
  lacks(head, 'APP_TITLE', 'say')
  assert(head.split('rel="icon"').length === 2, `the icon two routes give is written once: ${head}`)
  const app = await render('/hello', appData)
  contains(
    app.slice(0, app.indexOf('</head>')),
    '<title>APP_TITLE &amp; &lt;x&gt;</title>',
    '<meta name="description" content="say &quot;hi&quot; &amp; &lt;/script&gt;">',
  )
})

// documentStart is the document up to its head.
const documentStart = (html: string) => html.slice(0, html.indexOf('<head>'))

test('head sets attributes of <html> over those of the root Layout, the deepest route winning', async () => {
  const intl = { ...appData, 'app/intl/lang+/layout': ok('fr'), 'app/intl/lang+/route': ok('home') }
  const home = await render('/intl/fr', intl)
  assert(documentStart(home) === '<!DOCTYPE html><html lang="fr" data-theme="a&quot;b" class="outer" dir="ltr">', `document start: ${documentStart(home)}`)
  contains(home, 'INTL <!-- -->fr', 'INTL_HOME')
  // null takes away what an outer level set, and the attributes React DOM would not write are left
  // out. className and class are one attribute, written once.
  const rtl = await render('/intl/fr/rtl', { ...intl, 'app/intl/lang+/rtl/route': ok('rtl') })
  assert(
    documentStart(rtl) === '<!DOCTYPE html><html lang="fr" class="wide" dir="rtl" title="a&quot;&lt;b">',
    `document start: ${documentStart(rtl)}`,
  )
  contains(rtl.slice(0, rtl.indexOf('</head>')), '<title>RTL</title>')
  // head.test.tsx hydrates browser routers from this page and from that of /hello, which this
  // build cannot.
  writeFileSync('intl-fr-rtl.html', rtl)
  writeFileSync('hello.html', await render('/hello', appData))
  // The attributes of <html> are no head tags.
  lacks(rtl.slice(rtl.indexOf('<head>'), rtl.indexOf('</head>')), 'dir=', 'wide')
})

test('a failed or unmatched page gets the attributes of the levels it shows', async () => {
  // The Layout failed, so its head has no data and the root's lang stays.
  const failed = await render('/intl/fr', { ...appData, 'app/intl/lang+/layout': { status: 500, error: 'boom' }, 'app/intl/lang+/route': ok('home') })
  assert(documentStart(failed) === '<!DOCTYPE html><html lang="xx" data-theme="a&quot;b" class="outer" dir="ltr">', `failed Layout: ${documentStart(failed)}`)
  // The page failed below the Layout: the Layout's language holds.
  const missingPage = await render('/intl/fr', { ...appData, 'app/intl/lang+/layout': ok('fr'), 'app/intl/lang+/route': missing })
  assert(documentStart(missingPage) === '<!DOCTYPE html><html lang="fr" data-theme="a&quot;b" class="outer" dir="ltr">', `404 page: ${documentStart(missingPage)}`)
  contains(missingPage, 'APP_404')
  // A URL no route matches shows the root alone, with the root Layout's attributes.
  const unmatched = await render('/intl/fr/rtl/nowhere', {})
  assert(documentStart(unmatched) === '<!DOCTYPE html><html lang="xx" data-theme="a&quot;b" class="h-full">', `unmatched: ${documentStart(unmatched)}`)
})

test('a component that throws fails the render', async () => {
  const result = await renderResult('/throws', appData)
  assert(result.html === undefined && result.error !== undefined, `render /throws: ${JSON.stringify(result)}`)
})

test('a failed root Load keeps the document', async () => {
  const failed = await render('/hello', { 'app/layout': { status: 500, error: 'boom' } })
  contains(failed, '<html lang="xx" ', 'boom', 'src="/assets/app.js"', 'href="/assets/app.css"')
  const notFound = await render('/hello', { 'app/layout': missing })
  contains(notFound, '<html lang="xx" ', 'APP_404', 'src="/assets/app.js"')
})

test("a Layout's 404 is shown from the level above, inside the ancestors", async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': missing, 'app/teams/team+/one/route': ok('one') })
  contains(html, 'APP_NAV', 'APP_404')
  lacks(html, 'TEAM_404', 'TEAM_ONE')
})

test("a page's 404 is shown inside its Layouts", async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': missing })
  contains(html, 'APP_NAV', 'TEAM <!-- -->x', 'TEAM_404')
  lacks(html, 'TEAM_ONE')
})

test("a 404 below a Layout without NotFound is shown in that Layout's place", async () => {
  const html = await render('/a/two', { ...appData, 'app/a/layout': ok('a'), 'app/a/two/route': missing })
  contains(html, 'APP_NAV', 'APP_404')
  lacks(html, 'LAYOUT_A', 'PAGE_A_TWO')
})

test("an unmatched URL shows the root Layout's NotFound without asking for data", async () => {
  const { calls } = await client('/teams/x/missing')
  assert(calls.length === 0, `requested ${JSON.stringify(calls)}`)
  const html = await render('/teams/x/missing', {})
  contains(html, '<html lang="xx" ', 'APP_404', 'src="/assets/app.js"')
  // The root Layout did not load, its documentHead is on every page all the same.
  contains(html.slice(0, html.indexOf('</head>')), '<meta name="theme-color" content="#123">', '<link rel="icon" href="/icon.svg">')
})

test("the URL of a Layout without a page of its own is unmatched", async () => {
  for (const url of ['/a', '/teams/x']) {
    const { calls } = await client(url)
    assert(calls.length === 0, `${url} requested ${JSON.stringify(calls)}`)
    const html = await render(url, {})
    contains(html, 'APP_404')
    lacks(html, 'APP_NAV')
  }
})

test('the rest of the path may be empty, written without the trailing slash', async () => {
  const { router, calls } = await client('/docs')
  assert(calls.some(([u, id]) => u === '/docs' && id === 'app/docs/path++/route'), `calls ${JSON.stringify(calls)}`)
  assert((router.state.matches.at(-1)?.params as { _splat?: string })._splat === '', 'empty rest')
  contains(await render('/docs', { ...appData, 'app/docs/path++/route': ok('') }), 'DOC [<!-- -->]')
  // Go also answers /docs/, which the router writes as /docs.
  const slash = await renderResult('/docs/', { ...appData, 'app/docs/path++/route': ok('') })
  assert(slash.redirect?.location === '/docs', `/docs/ gives ${JSON.stringify(slash)}`)
  // Below a Layout without a page, the rest of the path takes the Layout's URL.
  contains(await render('/media', appData), 'MEDIA_LAYOUT', 'MEDIA_FILE')
  // Also when the rest of the path has a Layout of its own.
  for (const url of ['/library', '/library/a/b']) contains(await render(url, appData), 'LIBRARY_LAYOUT', 'SHELF_LAYOUT', 'BOOK')
})

// hydrateFrom runs the scripts of a server-rendered page the way a browser does and hydrates a
// browser router from them.
async function hydrateFrom(html: string, url: string) {
  const g = globalThis as any
  const saved = { window: g.window, self: g.self, document: g.document }
  g.window = g.self = globalThis
  try {
    for (const [, code] of html.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)) {
      if (code.trim() === '') continue
      g.document = { currentScript: { previousElementSibling: null, remove() {} }, querySelector: () => null }
      new Function(code)()
    }
    const { router, calls } = { calls: [] as Array<[string, string]>, router: undefined as any }
    const r = createAppRouter(
      routeTree,
      async (u, id) => {
        calls.push([u, id])
        return ok(id)
      },
      { history: createMemoryHistory({ initialEntries: [url] }) },
    )
    await hydrate(r)
    return { router: r, calls }
  } finally {
    Object.assign(g, saved)
  }
}

test("a page without its own Error shows the closest ancestor's", async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': { status: 403, error: 'no' } })
  contains(html, 'TEAM_ERROR <!-- -->403 no')
  const { router } = await client('/teams/x/one')
  const one = Object.values(router.routesById).find((r: any) => r.id.includes('/teams/$team/') && r.id.endsWith('/one')) as any
  const team = Object.values(router.routesById).find((r: any) => r.id.endsWith('/teams/$team')) as any
  assert(one.options.errorComponent === team.options.errorComponent, 'the page and its Layout use different Error components')
})

// titles lists the titles the head functions of the matches gave.
const titles = (router: any) =>
  router.state.matches.flatMap((m: any) => (m.meta ?? []).map((t: any) => t?.title).filter(Boolean))

test('head fills the matches after hydration and after a navigation', async () => {
  const url = '/teams/x/one'
  const html = await render(url, { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': ok('one') })
  const { router, calls } = await hydrateFrom(html, url)
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  const hydrated = JSON.stringify(titles(router))
  assert(hydrated === '["APP_TITLE & <x>","TEAM x","ONE_TITLE"]', `titles after hydration ${hydrated}`)
  // No React mounts the router here, so nothing loads the new location until load.
  await router.navigate({ to: '/teams/$team/one', params: { team: 'y' } })
  await router.load()
  const navigated = JSON.stringify(titles(router))
  assert(navigated === '["APP_TITLE & <x>","TEAM app/teams/team+/layout","ONE_TITLE"]', `titles after navigation ${navigated}`)
})

// The server bundle reads the modules that export head at once. The browser build of an
// application, which this test runs as when __LAZY_ROUTES__ is true, loads them when their route
// needs them, as it loads the other route modules, and keeps the root Layout in the entry script.
declare const __LAZY_ROUTES__: boolean
test('a route module that exports head loads with its route in the browser build', async () => {
  const { router } = await client('/hello')
  const routes = Object.values(router.routesById) as any[]
  const team = routes.find((r) => r.id.endsWith('/teams/$team'))
  const app = routes.find((r) => r.id === '/_app')
  assert((typeof team.options.component.preload === 'function') === __LAZY_ROUTES__, `the team Layout is lazy: ${!__LAZY_ROUTES__}`)
  assert(typeof app.options.component.preload !== 'function', 'the root Layout is lazy')
  const head = team.options.head({ loaderData: 'z' })
  assert((head instanceof Promise) === __LAZY_ROUTES__, `head gives a promise: ${head instanceof Promise}`)
  assert(JSON.stringify(await head) === '{"meta":[{"title":"TEAM z"}]}', `head gives ${JSON.stringify(await head)}`)
})

test('a RouteError keeps its class and status through hydration', async () => {
  const url = '/teams/x/one'
  const html = await render(url, { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': { status: 403, error: 'permission denied' } })
  const { router, calls } = await hydrateFrom(html, url)
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  const match = router.state.matches.find((m: any) => m.routeId.endsWith('/one'))
  assert(
    match?.error instanceof RouteError && match.error.status === 403 && match.error.message === 'permission denied',
    `error after hydration: ${match?.error} ${JSON.stringify(match?.error)}`,
  )
})

// richData is data a Go Load may return. It is read with JSON.parse, as the VM reads what Go
// sends, so __proto__ is a key of its own and not the prototype.
const richData = JSON.parse(
  JSON.stringify({
    name: 'router',
    numbers: [0, -1.5, 1e21, 9007199254740991],
    yes: true,
    no: false,
    none: null,
    text: '中文 \u2028 "quoted" \\ </script><!-- <script> $R',
    deep: { a: { b: { c: { d: { e: [[[{ f: 'g' }]]] } } } } },
    empty: {},
    list: [],
  }).replace(/}$/, ',"__proto__":{"polluted":true}}'),
)

test('loader data comes through hydration as the server had it', async () => {
  assert(Object.hasOwn(richData, '__proto__'), 'the test data lacks its own __proto__ key')
  const url = '/teams/x/one'
  // The page renders its data, so it is an array React can render.
  const one = ['one', [], '</script>']
  const html = await render(url, { 'app/layout': ok(richData), 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': ok(one) })
  contains(html, 'APP_NAV <!-- -->router', 'TEAM <!-- -->x', 'TEAM_ONE')
  const { router, calls } = await hydrateFrom(html, url)
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  // The first match is the router's own root route, which has no data.
  const loaded = router.state.matches.slice(1).map((m: any) => m.loaderData)
  assert(JSON.stringify(loaded[0]) === JSON.stringify(richData), `root data ${JSON.stringify(loaded[0])}`)
  assert(Object.getPrototypeOf(loaded[0]) === Object.prototype, 'root data has another prototype')
  assert(Object.hasOwn(loaded[0], '__proto__') && loaded[0].polluted === undefined, `__proto__ became ${Object.keys(loaded[0])}`)
  assert(({} as any).polluted === undefined, 'hydration changed Object.prototype')
  assert(JSON.stringify(loaded.slice(1)) === JSON.stringify(['x', one]), `route data ${JSON.stringify(loaded.slice(1))}`)
})

// JSON.stringify writes -0 as 0. The page carries the text Go wrote, so a -0 the server rendered
// reaches the browser as -0, and Intl writes the same -¥0.00 on both sides.
test('a -0 in loader data reaches the browser as the server read it', async () => {
  const url = '/teams/x/one'
  const html = await render(url, {
    'app/layout': { status: 200, json: '{"name":"router","refunded":-0,"list":[-0,1]}' },
    'app/teams/team+/layout': ok('x'),
    'app/teams/team+/one/route': ok(['one']),
  })
  contains(html, '\\"refunded\\":-0', '[-0,1]')
  const { router, calls } = await hydrateFrom(html, url)
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  const data = router.state.matches[1].loaderData as any
  assert(Object.is(data.refunded, -0) && Object.is(data.list[0], -0), `after hydration ${JSON.stringify(data)} ${Object.is(data.refunded, -0)}`)
})

// The text of a Load's data is an argument of startRender of its own, so it reaches the page as
// Go wrote it, whatever a JSON layer around it would have had to escape: quotes, backslashes,
// U+2028 and U+2029, </script>, and characters above U+FFFF that Go writes as surrogate pairs.
test('loader JSON text reaches the page as it was given, and the browser reads the same data', async () => {
  const url = '/teams/x/one'
  const tricky = '{"quote":"a \\"b\\"","slash":"C:\\\\temp","line":"x\u2028y\u2029","tag":"</script><!--","emoji":"\\ud83d\\ude00","accent":"é","nul":"\\u0000"}'
  const html = await render(url, {
    'app/layout': { status: 200, json: tricky },
    'app/teams/team+/layout': ok('x'),
    'app/teams/team+/one/route': ok(['one']),
  })
  contains(
    html,
    '$_TSR.t.get("tifa.data")("{\\"quote\\":\\"a \\\\\\"b\\\\\\"\\",\\"slash\\":\\"C:\\\\\\\\temp\\",\\"line\\":\\"x\\u2028y\\u2029\\",\\"tag\\":\\"\\x3C/script>\\x3C!--\\",' +
      '\\"emoji\\":\\"\\\\ud83d\\\\ude00\\",\\"accent\\":\\"é\\",\\"nul\\":\\"\\\\u0000\\"}")',
  )
  lacks(html, 'tag\\":\\"<', 'nul\\":\\"\0')
  const { router, calls } = await hydrateFrom(html, url)
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  const data = router.state.matches[1].loaderData as any
  assert(JSON.stringify(data) === JSON.stringify(JSON.parse(tricky)), `after hydration ${JSON.stringify(data)}`)
  assert(data.line === 'x\u2028y\u2029' && data.emoji === '\u{1F600}' && data.nul === '\0', `after hydration ${JSON.stringify(data)}`)
})

// A result that names a text startRender was not given cannot be rendered, and says which.
test('a text that startRender was not given fails the render', async () => {
  let message = ''
  try {
    globalThis.startRender(JSON.stringify({ url: '/hello', buildId: 'b1', data: { 'app/layout': { status: 200, text: 1 } } }), '{"name":"router"}')
  } catch (error) {
    message = String(error)
  }
  assert(message.includes('text 1') && message.includes('app/layout'), `the render says: ${message}`)
})

// The router reads serializationAdapters from its options, but its constructor type leaves them
// to TanStack Start and no doc covers setting them on a router of its own (TanStack/router#7796).
// If an update stops calling the adapter, loader data would go back to the slower default
// serializer, or break, without any other test noticing.
test('loader data is written as JSON through the tifa.data adapter', async () => {
  const html = await render('/hello', appData)
  contains(html, '$_TSR.t.get("tifa.data")("{\\"name\\":\\"router\\"}")')
  lacks(html, '{name:"router"}')
})

// seroval takes an array as an array before it asks the adapters, unless the SSR bundle changes it.
test('array loader data is written as JSON and comes back as the server had it', async () => {
  const url = '/teams/x/one'
  const one = ['one', [], [['two', null]], null, '</script>']
  for (const [data, text] of [
    [one, '$_TSR.t.get("tifa.data")("[\\"one\\",[],[[\\"two\\",null]],null,\\"\\x3C/script>\\"]")'],
    [[], '$_TSR.t.get("tifa.data")("[]")'],
    [null, ''],
  ] as const) {
    const html = await render(url, { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': ok(data) })
    contains(html, text, 'TEAM_ONE')
    lacks(html, '[[["two",null]]')
    const { router, calls } = await hydrateFrom(html, url)
    assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
    const loaded = router.state.matches.at(-1)!.loaderData as any
    assert(JSON.stringify(loaded) === JSON.stringify(data), `route data ${JSON.stringify(loaded)}`)
    if (data === null) {
      assert(loaded === null, `null became ${loaded}`)
      continue
    }
    assert(Array.isArray(loaded) && loaded.length === data.length, `array became ${JSON.stringify(loaded)}`)
    if (data === one)
      assert(Array.isArray(loaded[1]) && Array.isArray(loaded[2][0]) && loaded[2][0][1] === null && loaded[3] === null, `nested ${JSON.stringify(loaded)}`)
  }
})

test('404, redirect and RouteError results keep their paths', async () => {
  const url = '/teams/x/one'
  const html = await render(url, { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': missing })
  contains(html, 'TEAM_404')
  const { router, calls } = await hydrateFrom(html, url)
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  const states = router.state.matches.slice(1).map((m: any) => `${m.status} ${JSON.stringify(m.loaderData)}`)
  assert(
    JSON.stringify(states) === JSON.stringify(['success {"name":"router"}', 'success "x"', 'notFound undefined']),
    `states after hydration ${JSON.stringify(states)}`,
  )
  const redirected = await renderResult('/hello', { 'app/layout': { status: 302, location: '/login' } })
  assert(JSON.stringify(redirected.redirect) === '{"location":"/login","status":302}', `redirect ${JSON.stringify(redirected)}`)
  const failed = await render('/hello', { 'app/layout': { status: 403, error: 'no' } })
  const hydrated = await hydrateFrom(failed, '/hello')
  const error = hydrated.router.state.matches[1].error
  assert(error instanceof RouteError && error.status === 403 && error.message === 'no', `root error ${error}`)
})

test('preloaded data never goes stale by itself', async () => {
  const { router } = await client('/hello')
  for (const route of Object.values(router.routesById) as Array<{ id: string; options: any }>) {
    if (!route.options.loader) continue
    assert(route.options.staleTime === Infinity, `${route.id} staleTime ${route.options.staleTime}`)
    assert(route.options.preloadStaleTime === Infinity, `${route.id} preloadStaleTime ${route.options.preloadStaleTime}`)
  }
})

// stubFetch answers every request with status and body and records the requests.
function stubFetch(status: number, body: unknown) {
  const seen: string[] = []
  const options: RequestInit[] = []
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    seen.push(url)
    options.push(init)
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
  }) as typeof fetch
  return Object.assign(seen, { options })
}

test('a grouped answer reaches every route whatever the HTTP status', async () => {
  stubFetch(404, { 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': missing })
  const source = collector(() => 'b1')
  const [team, one] = await Promise.all([source('/teams/x/one', 'app/teams/team+/layout'), source('/teams/x/one', 'app/teams/team+/one/route')])
  assert(team.status === 200 && team.data === 'x', `team ${JSON.stringify(team)}`)
  assert(one.status === 404, `one ${JSON.stringify(one)}`)

  stubFetch(500, { error: 'down' })
  const failed = await source('/teams/x/one', 'app/teams/team+/layout').then(
    () => undefined,
    (error) => error,
  )
  assert(failed instanceof RouteError && failed.status === 500 && failed.message === 'down', `failure ${failed}`)
})

test('a redirect of the server reaches every route as a redirect', async () => {
  const requests = stubFetch(200, { redirect: '/login', status: 302 })
  const source = collector(() => 'b1')
  const results = await Promise.all([source('/teams/x/one', 'app/teams/team+/layout'), source('/teams/x/one', 'app/teams/team+/one/route')])
  for (const result of results)
    assert(result.status === 302 && result.location === '/login', `result ${JSON.stringify(result)}`)
  assert(requests.options[0]?.redirect === 'manual', `fetch follows redirects: ${JSON.stringify(requests.options[0])}`)
  // The loader turns it into a router redirect to the target.
  const thrown = await load({ source: async () => results[0] }, 'app/teams/team+/layout', { pathname: '/teams/x/one', searchStr: '' }).then(
    () => undefined,
    (error) => error,
  )
  assert(isRedirect(thrown) && thrown.options.href === '/login', `loader threw ${thrown}`)
})

test('a 409 reloads the current page once', async () => {
  const location = { reloads: 0, assigned: [] as string[] }
  ;(globalThis as any).window = {
    location: { reload: () => location.reloads++, assign: (url: string) => location.assigned.push(url) },
  }
  stubFetch(409, { error: 'the application was updated' })
  const source = collector(() => 'old')
  void source('/hovered', 'app/a/layout')
  void source('/clicked', 'app/b/layout')
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert(location.reloads === 1, `reloads ${location.reloads}`)
  assert(location.assigned.length === 0, `assigned ${location.assigned}`)
})

let failed = 0
for (const [name, fn] of tests) {
  try {
    await fn()
    console.log(`ok   ${name}`)
  } catch (error) {
    failed++
    console.log(`FAIL ${name}\n${error instanceof Error ? error.stack : error}`)
  }
}
if (failed > 0) throw new Error(`${failed} of ${tests.length} tests failed`)
