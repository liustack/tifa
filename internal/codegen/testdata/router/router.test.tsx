// Run by TestGeneratedRouterBehaves in Node, against the route tree generated for this app.
import { writeFileSync } from 'node:fs'
import { createMemoryHistory, isRedirect } from '@tanstack/react-router'
import { hydrate } from '@tanstack/react-router/ssr/client'
import { htmlAttributes, routeTree } from './.tifa/routes'
import { createAppRouter, load, unmatched, type Dehydrated } from './.tifa/runtime/router'
import { serve, type RenderResult } from './.tifa/runtime/server'
import { headHTML, headTags, htmlAttributesOf } from './.tifa/runtime/head'
import { collector, RouteError, type InjectedResult, type RouteResult } from './.tifa/runtime/data'
import { resetReloads, watchChunks } from './.tifa/runtime/skew'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}
const ok = (data: unknown): RouteResult => ({ status: 200, data })
const missing: RouteResult = { status: 404, error: 'Not Found' }

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
async function renderResult(url: string, data: Record<string, RouteResult | InjectedResult>, preload?: string, nonce?: string, slotMin?: number, extra: object = {}) {
  globalThis.renderResult = undefined
  const input = inject(data)
  globalThis.startRender(JSON.stringify({ url, buildId: 'b1', data: input.data, preload, nonce, slotMin, ...extra }), ...input.texts)
  for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++)
    await new Promise((resolve) => setTimeout(resolve, 0))
  const result = globalThis.renderResult ?? '{"error":"render did not finish"}'
  // A page comes as the HTML itself, anything else as JSON.
  return (result.startsWith('<') ? { html: result, texts: input.texts } : JSON.parse(result)) as {
    html?: string
    texts?: string[]
    redirect?: { location: string; status: number }
    error?: string
  }
}

async function render(url: string, data: Record<string, RouteResult | InjectedResult>, preload?: string, nonce?: string, slotMin?: number, extra: object = {}): Promise<string> {
  const result = await renderResult(url, data, preload, nonce, slotMin, extra)
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
  assert(redirected.redirect?.location === '/e' && redirected.redirect.status === 308, `render /%65: ${JSON.stringify(redirected)}`)
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
  // Without preloads the document is the one written before there were any.
  const plain = await render('/teams/x/one', data)
  assert(html.replace(preload, '') === plain, `the preloads change more than their place:\n${html}\n${plain}`)
})

// The router stamps each match with the time it loaded and writes that time into the page. The
// render writes 0 instead, so the page is a function of its input, which the render cache and
// the ETag stand on, and the browser dates the matches itself.
test('two renders of the same data at different times write the same page', async () => {
  const data = { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': ok('one') }
  const first = await render('/teams/x/one', data)
  const now = Date.now
  Date.now = () => now() + 130033000
  let second: string
  try {
    second = await render('/teams/x/one', data)
  } finally {
    Date.now = now
  }
  assert(first === second, `the pages differ:\n${first}\n${second}`)
  assert(/\bu:0\b/.test(first) && !/\bu:[1-9]/.test(first), `the matches carry a time: ${first}`)
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
  // A URL no page matches shows inside the deepest Layout it matches, with that Layout's
  // attributes: below the language Layout, the 404 is in that language.
  const unmatched = await render('/intl/fr/rtl/nowhere', { ...appData, 'app/intl/lang+/layout': ok('fr') })
  assert(documentStart(unmatched) === '<!DOCTYPE html><html lang="fr" data-theme="a&quot;b" class="outer" dir="ltr">', `unmatched: ${documentStart(unmatched)}`)
  contains(unmatched, 'INTL <!-- -->fr', 'APP_404')
})

test('a component that throws fails the render', async () => {
  const result = await renderResult('/throws', appData)
  assert(result.html === undefined && result.error !== undefined, `render /throws: ${JSON.stringify(result)}`)
})

test('a render that failed reports its levels, and renders again with the error placed at a level', async () => {
  const failed = await renderResult('/throws', appData) as { levels?: number }
  assert(failed.levels === 3, `levels of /throws: ${JSON.stringify(failed)}`)
  const placed = async (level: number) => {
    globalThis.renderResult = undefined
    const input = inject(appData)
    globalThis.startRender(JSON.stringify({ url: '/throws', buildId: 'b1', data: input.data, error: { status: 503, message: 'down', level } }), ...input.texts)
    for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++) await new Promise((resolve) => setTimeout(resolve, 0))
    return globalThis.renderResult!
  }
  // Level 0 is the page: its Error, the root Layout's, shows inside the root Layout.
  const page = await placed(0)
  contains(page, '<html lang="xx" ', 'APP_NAV', 'APP_ERROR <!-- -->503 down', '<title>APP_TITLE &amp; &lt;x&gt;</title>')
  lacks(page, 'PAGE_THROWS')
  const { router, calls } = await hydrateFrom(page, '/throws')
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  const match = router.state.matches[router.state.matches.length - 1]
  assert(match.status === 'error' && match.error instanceof RouteError && match.error.status === 503, `after hydration: ${match.status} ${match.error}`)
  // Level 1 is the root Layout: the framework's Error shows in its place.
  const layout = await placed(1)
  contains(layout, '<html lang="xx" ', 'Something went wrong', 'down')
  lacks(layout, 'APP_NAV', 'APP_ERROR')
  contains(await placed(2), 'Something went wrong', 'down')
  const beyond = JSON.parse(await placed(3))
  assert(typeof beyond.error === 'string' && beyond.error.includes('no level 3'), `level 3: ${JSON.stringify(beyond)}`)
})

// placedRender renders url again with an error placed at a level, as the Go server does after a
// render failed, and returns what the render left.
async function placedRender(url: string, data: Record<string, RouteResult | InjectedResult>, level: number) {
  globalThis.renderResult = undefined
  const input = inject(data)
  globalThis.startRender(JSON.stringify({ url, buildId: 'b1', data: input.data, error: { status: 503, message: 'down', level } }), ...input.texts)
  for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++) await new Promise((resolve) => setTimeout(resolve, 0))
  return globalThis.renderResult!
}

const headOf = (html: string) => html.slice(html.indexOf('<head>'), html.indexOf('</head>'))

test('an error placed at a level takes the head of the routes it replaces, as a failed Load does', async () => {
  const data = { ...appData, 'app/crash/layout': ok('shell'), 'app/crash/boom/route': ok('page') }
  const thrown = await renderResult('/crash/boom', data)
  assert(thrown.html === undefined && thrown.error?.includes('CRASH_PAGE_THROWS'), `the page throws: ${JSON.stringify(thrown).slice(0, 200)}`)
  // The page is replaced: its head runs without data, as when its Load fails, and gives nothing.
  // The Layout around it keeps its tags, its language and its title.
  const page = await placedRender('/crash/boom', data, 0)
  const failedLoad = await render('/crash/boom', { ...data, 'app/crash/boom/route': { status: 503, error: 'down' } })
  contains(headOf(page), '<title>CRASH_LAYOUT_TITLE</title>', '<link rel="alternate" href="/crash/layout">')
  lacks(headOf(page), 'CRASH_PAGE_TITLE', 'rel="canonical"')
  contains(documentStart(page), 'lang="crash"')
  lacks(documentStart(page), 'dir="rtl"')
  assert(headOf(page) === headOf(failedLoad), `head of the placed error:\n${headOf(page)}\nhead of the failed Load:\n${headOf(failedLoad)}`)
  assert(documentStart(page) === documentStart(failedLoad), `<html> of the placed error: ${documentStart(page)}\nof the failed Load: ${documentStart(failedLoad)}`)
  // The Layout is replaced too, so only the root Layout's tags and language are left.
  const layout = await placedRender('/crash/boom', data, 1)
  const failedLayout = await render('/crash/boom', { ...data, 'app/crash/layout': { status: 503, error: 'down' } })
  contains(headOf(layout), '<title>APP_TITLE &amp; &lt;x&gt;</title>')
  lacks(headOf(layout), 'CRASH_', 'rel="alternate"', 'rel="canonical"')
  contains(documentStart(layout), 'lang="xx"')
  assert(headOf(layout) === headOf(failedLayout), `head of the placed error:\n${headOf(layout)}\nhead of the failed Layout:\n${headOf(failedLayout)}`)
  assert(documentStart(layout) === documentStart(failedLayout), `<html>: ${documentStart(layout)} and ${documentStart(failedLayout)}`)
  // The browser takes the page as the server wrote it: its head is the one in the page.
  for (const [html, level] of [[page, 0], [layout, 1]] as const) {
    const { router, calls } = await hydrateFrom(html, '/crash/boom')
    assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)} at level ${level}`)
    const tags = headHTML(headTags(router.state.matches))
    const written = headOf(html).replace(/^<head>.*?<!--tifa-head-->/s, '').replace(/<!--\/tifa-head-->$/, '')
    assert(tags === written, `level ${level}: the browser's head:\n${tags}\nthe server's:\n${written}`)
  }
})

test('a failed root Load keeps the document', async () => {
  const failed = await render('/hello', { 'app/layout': { status: 500, error: 'boom' } })
  contains(failed, '<html lang="xx" ', 'boom', 'src="/assets/app.js"', 'href="/assets/app.css"')
  // The root Layout's Error shows inside the root Layout, so the framework's shows its failure.
  const notFound = await render('/hello', { 'app/layout': missing })
  contains(notFound, '<html lang="xx" ', 'Page not found', 'src="/assets/app.js"')
  lacks(notFound, 'APP_404')
})

test("a failed Layout shows the Error of the level above, inside the ancestors", async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': missing, 'app/teams/team+/one/route': ok('one') })
  contains(html, 'APP_NAV', 'APP_404')
  lacks(html, 'TEAM_404', 'TEAM_ONE')
  const failed = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': { status: 403, error: 'no' }, 'app/teams/team+/one/route': ok('one') })
  contains(failed, 'APP_NAV', 'APP_ERROR <!-- -->403 no')
  lacks(failed, 'TEAM_ERROR')
})

test("a page's 404 is shown inside its Layouts", async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': missing })
  contains(html, 'APP_NAV', 'TEAM <!-- -->x', 'TEAM_404')
  lacks(html, 'TEAM_ONE')
})

test("a 404 below a Layout without Error is shown with the Error above, inside that Layout", async () => {
  const html = await render('/a/two', { ...appData, 'app/a/layout': ok('a'), 'app/a/two/route': missing })
  contains(html, 'APP_NAV', 'LAYOUT_A', 'APP_404')
  lacks(html, 'PAGE_A_TWO')
})

test('an unmatched URL shows the 404 inside the deepest Layout it matches, after loading the Layouts', async () => {
  const { calls } = await client('/teams/x/missing')
  const ids = calls.map(([, id]) => id).sort()
  assert(JSON.stringify(ids) === '["app/layout","app/teams/team+/layout"]', `requested ${JSON.stringify(calls)}`)
  const html = await render('/teams/x/missing', { ...appData, 'app/teams/team+/layout': ok('x') })
  contains(html, '<html lang="xx" ', 'APP_NAV', 'TEAM <!-- -->x', 'TEAM_404', 'src="/assets/app.js"')
  contains(html.slice(0, html.indexOf('</head>')), '<meta name="theme-color" content="#123">', '<link rel="icon" href="/icon.svg">', '<title>TEAM x</title>')
  // A literal page is more specific than a Layout of a parameter: /a/one/x is under the root
  // Layout, not under /teams/{team}. The page itself does not load.
  const below = await client('/a/one/x')
  assert(JSON.stringify(below.calls.map(([, id]) => id).sort()) === '["app/a/layout","app/layout"]', `requested ${JSON.stringify(below.calls)}`)
  contains(await render('/a/one/x', { ...appData, 'app/a/layout': ok('a') }), 'LAYOUT_A', 'APP_404')
  // Outside every Layout but the root one.
  const outside = await render('/nowhere/at/all', appData)
  contains(outside, 'APP_NAV', 'APP_404')
  // The Layout that does not load shows the Error above it.
  const failed = await render('/teams/x/missing', { ...appData, 'app/teams/team+/layout': { status: 500, error: 'boom' } })
  contains(failed, 'APP_NAV', 'APP_ERROR <!-- -->500 boom')
  lacks(failed, 'TEAM_404')
})

test('the URL of a Layout without a page of its own shows the 404 inside that Layout', async () => {
  for (const [url, data, inside] of [
    ['/a', { 'app/a/layout': ok('a') }, 'LAYOUT_A'],
    ['/teams/x', { 'app/teams/team+/layout': ok('x') }, 'TEAM <!-- -->x'],
    ['/', {}, 'APP_NAV'],
  ] as const) {
    const { calls } = await client(url)
    const ids = calls.map(([, id]) => id).sort()
    assert(JSON.stringify(ids) === JSON.stringify(['app/layout', ...Object.keys(data)].sort()), `${url} requested ${JSON.stringify(calls)}`)
    const html = await render(url, { ...appData, ...data })
    contains(html, 'APP_NAV', inside, url === '/teams/x' ? 'TEAM_404' : 'APP_404')
  }
})

// app/solo has layout_.tsx: its pages show without the root Layout, which does not load for them.
test('a layout_.tsx shows its pages without the root Layout, which does not load for them', async () => {
  for (const [url, want] of [
    ['/solo', '["app/solo/layout"]'],
    ['/solo/inner', '["app/solo/layout"]'],
    ['/solo/nope', '["app/solo/layout"]'],
  ] as const) {
    const { calls } = await client(url)
    const ids = JSON.stringify(calls.map(([, id]) => id).sort())
    assert(ids === want, `${url} requested ${ids}`)
  }
  const html = await render('/solo/inner', { 'app/solo/layout': ok('s') })
  contains(html, '<html lang="xx" ', 'SOLO <!-- -->s', 'SOLO_INNER_LAYOUT', 'SOLO_INNER')
  lacks(html, 'APP_NAV')
  // The document head of the root Layout stays, its head does not.
  const head = html.slice(0, html.indexOf('</head>'))
  contains(head, '<meta name="theme-color" content="#123">', '<link rel="icon" href="/icon.svg">')
  lacks(head, 'APP_TITLE', 'say ')
  // A URL below it that no page has shows its 404 inside it.
  const missingPage = await render('/solo/nope', { 'app/solo/layout': ok('s') })
  contains(missingPage, 'SOLO <!-- -->s', 'SOLO_404')
  lacks(missingPage, 'APP_NAV', 'APP_404')
  // When it fails, nothing is above it but the framework's Error.
  contains(await render('/solo', { 'app/solo/layout': missing }), '<main><h1>Page not found</h1></main>')
  const failed = await render('/solo', { 'app/solo/layout': { status: 503, error: 'later' } })
  contains(failed, '<h1>Something went wrong</h1><p>later</p>')
  lacks(failed, 'APP_NAV', 'APP_ERROR')
})

test('navigating across a layout_.tsx swaps the outer Layouts and loads what is missing', async () => {
  const outer = (router: any) => router.state.matches.map((m: any) => m.routeId).filter((id: string) => id === '/_app' || id === '/solo')
  const { router, calls } = await client('/solo')
  assert(JSON.stringify(outer(router)) === '["/solo"]', `matches ${JSON.stringify(outer(router))}`)
  calls.length = 0
  // No React mounts the router here, so nothing loads the new location until load.
  router.history.push('/teams/x/one')
  await router.load()
  assert(JSON.stringify(outer(router)) === '["/_app"]', `matches ${JSON.stringify(outer(router))}`)
  const ids = JSON.stringify(calls.map(([, id]) => id).sort())
  assert(ids === '["app/layout","app/teams/team+/layout","app/teams/team+/one/route"]', `to the team requested ${ids}`)
  // Back below layout_.tsx: the root Layout is not asked for.
  calls.length = 0
  router.history.push('/solo/inner')
  await router.load()
  assert(JSON.stringify(outer(router)) === '["/solo"]', `matches ${JSON.stringify(outer(router))}`)
  assert(!calls.some(([, id]) => id === 'app/layout'), `back requested ${JSON.stringify(calls)}`)
  // A page rendered on the server hydrates without requests, then leaves for the root Layout.
  const html = await render('/solo/inner', { 'app/solo/layout': ok('s') })
  const hydrated = await hydrateFrom(html, '/solo/inner')
  assert(hydrated.calls.length === 0, `hydration requested ${JSON.stringify(hydrated.calls)}`)
  hydrated.router.history.push('/hello')
  await hydrated.router.load()
  assert(JSON.stringify(outer(hydrated.router)) === '["/_app"]', `matches ${JSON.stringify(outer(hydrated.router))}`)
  assert(JSON.stringify(hydrated.calls.map(([, id]) => id)) === '["app/layout"]', `to /hello requested ${JSON.stringify(hydrated.calls)}`)
})

test("the framework's Error shows a 404 as a page that is not found", async () => {
  const html = await render('/hello', { 'app/layout': missing })
  contains(html, '<main><h1>Page not found</h1></main>')
  const failed = await render('/hello', { 'app/layout': { status: 503, error: 'later' } })
  contains(failed, '<h1>Something went wrong</h1><p>later</p>')
})

test('the rest of the path may be empty, written without the trailing slash', async () => {
  const { router, calls } = await client('/docs')
  assert(calls.some(([u, id]) => u === '/docs' && id === 'app/docs/path++/route'), `calls ${JSON.stringify(calls)}`)
  assert((router.state.matches.at(-1)?.params as { _splat?: string })._splat === '', 'empty rest')
  contains(await render('/docs', { ...appData, 'app/docs/path++/route': ok('') }), 'DOC [<!-- -->]')
  // Go also answers /docs/, which the router writes as /docs.
  const slash = await renderResult('/docs/', { ...appData, 'app/docs/path++/route': ok('') })
  assert(slash.redirect?.location === '/docs' && slash.redirect.status === 308, `/docs/ gives ${JSON.stringify(slash)}`)
  // Below a Layout without a page, the rest of the path takes the Layout's URL.
  contains(await render('/media', appData), 'MEDIA_LAYOUT', 'MEDIA_FILE')
  // Also when the rest of the path has a Layout of its own.
  for (const url of ['/library', '/library/a/b']) contains(await render(url, appData), 'LIBRARY_LAYOUT', 'SHELF_LAYOUT', 'BOOK')
})

// hydrateFrom runs the scripts of a server-rendered page the way a browser does and hydrates a
// browser router from them.
async function hydrateFrom(html: string, url: string, onHydrate?: (dehydrated: Dehydrated) => void) {
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
      { history: createMemoryHistory({ initialEntries: [url] }), onHydrate },
    )
    await hydrate(r)
    return { router: r, calls }
  } finally {
    Object.assign(g, saved)
  }
}

test("a page without its own Error shows the closest ancestor's", async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': { status: 403, error: 'no' } })
  contains(html, 'TEAM <!-- -->x', 'TEAM_ERROR <!-- -->403 no')
  const { router } = await client('/teams/x/one')
  const one = Object.values(router.routesById).find((r: any) => r.id.includes('/teams/$team/') && r.id.endsWith('/one')) as any
  const two = Object.values(router.routesById).find((r: any) => r.id.includes('/teams/$team/') && r.id.endsWith('/two')) as any
  const team = Object.values(router.routesById).find((r: any) => r.id.endsWith('/teams/$team')) as any
  assert(one.options.errorComponent === two.options.errorComponent, 'the pages of a Layout use different Error components')
  assert(one.options.errorComponent !== team.options.errorComponent, 'the Layout shows its own Error in its place')
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

// With a nonce in the input, which the Go pool draws for each render, the page carries a slot in
// place of the text of each object a Load returned, and Go fills the slot once the VM is free. A
// page whose slots are filled with the text, written as seroval writes a string, is the page
// rendered without a nonce, byte for byte, and the browser reads the same data from it.
const slotNonce = '0123456789abcdef0123456789abcdef'

test('with a nonce the data text is a slot, and the rest of the page is the page without one', async () => {
  const url = '/teams/x/one'
  const texts = [
    '{"quote":"a \\"b\\"","slash":"C:\\\\temp","line":"x\u2028y\u2029","tag":"</script><!--","emoji":"\\ud83d\\ude00","accent":"é"}',
    '["x"]',
    '["one","two"]',
  ]
  const data = {
    'app/layout': { status: 200, json: texts[0] },
    'app/teams/team+/layout': { status: 200, json: texts[1] },
    'app/teams/team+/one/route': { status: 200, json: texts[2] },
  }
  const plain = await render(url, data)
  const slotted = await render(url, data, undefined, slotNonce, 0)
  const slots = texts.map((_, number) => `~${slotNonce}.${number}.`)
  for (const slot of slots) contains(slotted, `$_TSR.t.get("tifa.data")("${slot}")`)
  lacks(slotted, 'quote', 'accent', 'emoji')
  assert(slotted.split(slotNonce).length === slots.length + 1, `the nonce is in ${slotted.split(slotNonce).length - 1} places`)
  // The two pages are the same around the slots, and the plain one has a JavaScript string where a slot is.
  const around = slotted.split(new RegExp(slots.map((s) => s.replace(/\./g, '\\.')).join('|')))
  assert(plain.startsWith(around[0]) && plain.endsWith(around[around.length - 1]), 'the pages differ outside the slots')
  let at = around[0].length
  let filled = around[0]
  for (const [number, text] of texts.entries()) {
    const next = around[number + 1]
    const end = number === texts.length - 1 ? plain.length - next.length : plain.indexOf(next, at)
    assert(end >= at, `the pages differ after slot ${number}`)
    const literal = plain.slice(at, end)
    // JavaScript reads the string of the plain page as the text.
    let value: unknown
    try {
      value = new Function(`return "${literal}"`)()
    } catch (error) {
      throw new Error(`slot ${number}: ${error} in ${literal.slice(0, 200)} ... ${literal.slice(-200)}`)
    }
    assert(value === text, `the plain page has another text for slot ${number}`)
    filled += literal + next
    at = end + next.length
  }
  // Fill the slots with those strings, and the browser reads the data from the page.
  assert(filled === plain, 'filling the slots does not give the plain page')
  const { router, calls } = await hydrateFrom(filled, url)
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  const loaded = router.state.matches.slice(1).map((m: any) => m.loaderData)
  assert(JSON.stringify(loaded) === JSON.stringify(texts.map((t) => JSON.parse(t))), `data after hydration ${JSON.stringify(loaded)}`)
})

// The scripts of a page split where the router splits them: a text over 16K code units goes into a
// script tag of its own. A slot is short, so the same page has fewer tags, and its data is the
// same in the browser.
test('a page with a large text in a slot hydrates to the same data', async () => {
  const url = '/teams/x/one'
  const rows = Array.from({ length: 3000 }, (_, i) => ({ id: i, name: `row ${i}`, note: 'a "b" c\\d' }))
  const text = JSON.stringify(rows)
  const data = {
    'app/layout': { status: 200, json: text },
    'app/teams/team+/layout': ok('x'),
    'app/teams/team+/one/route': ok('one'),
  }
  const plain = await renderResult(url, data)
  const slots = await renderResult(url, data, undefined, slotNonce)
  assert(plain.html !== undefined && slots.html !== undefined, `renders ${JSON.stringify([plain, slots])}`)
  assert(plain.html.length > 100000 && slots.html.length < 20000, `lengths ${plain.html.length} ${slots.html.length}`)
  const filled = slots.html.replace(`~${slotNonce}.0.`, () => JSON.stringify(text).slice(1, -1))
  const { router, calls } = await hydrateFrom(filled, url)
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  const loaded = router.state.matches[1].loaderData as any
  assert(JSON.stringify(loaded) === text, `hydrated ${JSON.stringify(loaded).slice(0, 200)}`)
})

// Go fills a slot by copying the page once more, which a short text does not pay back in the VM.
// From 4,096 code units a text goes into the page as a slot, and a shorter one is written as it was.
test('only a long text becomes a slot', async () => {
  const url = '/teams/x/one'
  const short = JSON.stringify(['short'])
  const long = JSON.stringify({ note: 'y'.repeat(4100) })
  assert(short.length < 4096 && long.length >= 4096, `lengths ${short.length} and ${long.length}`)
  const data = {
    'app/layout': { status: 200, json: long },
    'app/teams/team+/layout': ok('x'),
    'app/teams/team+/one/route': { status: 200, json: short },
  }
  const html = await render(url, data, undefined, slotNonce)
  contains(html, `$_TSR.t.get("tifa.data")("~${slotNonce}.0.")`, `$_TSR.t.get("tifa.data")("[\\"short\\"]")`)
  lacks(html, 'y'.repeat(100), `~${slotNonce}.2.`)
  assert(html.split(slotNonce).length === 2, `the nonce is in ${html.split(slotNonce).length - 1} places`)
})

// Data that is a string, a number or null goes through the default serializer and carries its value
// in the page, nonce or not. Only an object or an array is a slot.
test('a nonce changes nothing for data that is no object', async () => {
  const data = { 'app/layout': { status: 200, json: '"a \\"string\\" with </script>"' }, 'app/teams/team+/layout': { status: 200, json: '42' }, 'app/teams/team+/one/route': { status: 200, json: 'null' } }
  const plain = await render('/teams/x/one', data)
  const slots = await render('/teams/x/one', data, undefined, slotNonce, 0)
  assert(plain === slots, 'the pages differ')
  lacks(slots, slotNonce)
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
    JSON.stringify(states) === JSON.stringify(['success {"name":"router"}', 'success "x"', 'error undefined']),
    `states after hydration ${JSON.stringify(states)}`,
  )
  const notFound = router.state.matches.at(-1)?.error
  assert(notFound instanceof RouteError && notFound.status === 404 && notFound.message === 'Not Found', `404 after hydration ${notFound}`)
  const redirected = await renderResult('/hello', { 'app/layout': { status: 302, location: '/login' } })
  assert(JSON.stringify(redirected.redirect) === '{"location":"/login","status":302}', `redirect ${JSON.stringify(redirected)}`)
  // A redirect a Load asked for keeps the status it asked for, 307 included.
  const temporary = await renderResult('/hello', { 'app/layout': { status: 307, location: '/login' } })
  assert(JSON.stringify(temporary.redirect) === '{"location":"/login","status":307}', `307 redirect ${JSON.stringify(temporary)}`)
  const failed = await render('/hello', { 'app/layout': { status: 403, error: 'no' } })
  const hydrated = await hydrateFrom(failed, '/hello')
  const error = hydrated.router.state.matches[1].error
  assert(error instanceof RouteError && error.status === 403 && error.message === 'no', `root error ${error}`)
})

test('loaders keep the freshness defaults of the router, stale at once and loaded again in the background', async () => {
  const { router } = await client('/hello')
  assert(router.options.defaultStaleReloadMode !== 'blocking', `stale reload mode ${router.options.defaultStaleReloadMode}`)
  for (const route of Object.values(router.routesById) as Array<{ id: string; options: any }>) {
    if (!route.options.loader || route.options.loader === unmatched) continue
    assert(route.options.staleTime === undefined, `${route.id} staleTime ${route.options.staleTime}`)
    assert(route.options.preloadStaleTime === undefined, `${route.id} preloadStaleTime ${route.options.preloadStaleTime}`)
  }
})

// stubFetch answers every request with status and body and records the requests.
function stubFetch(status: number, body: unknown, headers: Record<string, string> = {}) {
  const seen: string[] = []
  const options: RequestInit[] = []
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    seen.push(url)
    options.push(init)
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } })
  }) as typeof fetch
  return Object.assign(seen, { options })
}

test('the router names the Loads of a URL from the root Layout down', async () => {
  const { router } = await client('/a/one')
  const chain = (path: string) => router.options.context.chain(path).join(',')
  assert(chain('/a/one') === 'app/layout,app/a/layout', `/a/one: ${chain('/a/one')}`)
  assert(chain('/a/two') === 'app/layout,app/a/layout,app/a/two/route', `/a/two: ${chain('/a/two')}`)
  // A URL below a Layout that no page has shows the Layout's 404 after the Layouts above it.
  assert(chain('/a/nowhere') === 'app/layout,app/a/layout', `/a/nowhere: ${chain('/a/nowhere')}`)
  // So does a URL that only starts with a page's path: the page is not on the chain, and neither
  // is the Load of a page below the Layout that shows the 404.
  assert(chain('/a/two/x') === 'app/layout,app/a/layout', `/a/two/x: ${chain('/a/two/x')}`)
  assert(chain('/a/two/') === 'app/layout,app/a/layout,app/a/two/route', `/a/two/: ${chain('/a/two/')}`)
  // The collector counts its digits against that chain.
  const requests = stubFetch(200, { routes: { 'app/a/layout': ok('x') } })
  await collector(() => 'b1')('/a/two/x', 'app/a/layout', { chain: () => router.options.context.chain('/a/two/x'), preload: false })
  assert(requests[0] === '/a/two/x?_tifa=b1.01', `request ${requests[0]}`)
})

// alone is the slot of a load whose URL has no other Load. teamChain is that of /teams/x/one.
const alone = (id: string, preload = false) => ({ chain: () => [id], preload })
const teamChain = { chain: () => ['app/layout', 'app/teams/team+/layout', 'app/teams/team+/one/route'], preload: false }

test('a data request is the page URL with _tifa and no header of tifa', async () => {
  const requests = stubFetch(200, {
    routes: { 'app/layout': ok('x'), 'app/teams/team+/layout': ok('t'), 'app/teams/team+/one/route': ok('y') },
  })
  const source = collector(() => 'b1')
  await Promise.all([source('/teams/x/one', 'app/layout', teamChain), source('/teams/x/one', 'app/teams/team+/one/route', teamChain)])
  assert(requests.length === 1 && requests[0] === '/teams/x/one?_tifa=b1.101', `requests ${requests}`)
  const headers = new Headers(requests.options[0].headers)
  assert([...headers.keys()].join() === 'accept', `headers ${[...headers.keys()]}`)

  // A query stays as it is, the parameter comes after it.
  await source('/teams/x/one?a=1&b=x+y', 'app/teams/team+/layout', teamChain)
  assert(requests[1] === '/teams/x/one?a=1&b=x+y&_tifa=b1.010', `query ${requests[1]}`)

  // A preload adds .p. A load that is not a preload in the same request makes it a plain one.
  await source('/teams/x/one', 'app/layout', { ...teamChain, preload: true })
  assert(requests[2] === '/teams/x/one?_tifa=b1.100.p', `preload ${requests[2]}`)
  await Promise.all([
    source('/teams/x/one', 'app/layout', { ...teamChain, preload: true }),
    source('/teams/x/one', 'app/teams/team+/layout', teamChain),
  ])
  assert(requests[3] === '/teams/x/one?_tifa=b1.110', `a preload and a navigation ${requests[3]}`)

  // A load that is not on the chain of its URL is a bug of the page, not a request.
  const off = await source('/teams/x/one', 'app/other', teamChain).then(
    () => undefined,
    (error) => error,
  )
  assert(off instanceof Error && [...requests].length === 4, `off the chain: ${off}, requests ${requests}`)
})

test('a grouped answer reaches every route whatever the HTTP status', async () => {
  stubFetch(404, { routes: { 'app/teams/team+/layout': ok('x'), 'app/teams/team+/one/route': missing } })
  const source = collector(() => 'b1')
  const [team, one] = await Promise.all([source('/teams/x/one', 'app/teams/team+/layout', teamChain), source('/teams/x/one', 'app/teams/team+/one/route', teamChain)])
  assert(team.status === 200 && team.data === 'x', `team ${JSON.stringify(team)}`)
  assert(one.status === 404, `one ${JSON.stringify(one)}`)

  stubFetch(500, { error: 'down' })
  const failed = await source('/teams/x/one', 'app/teams/team+/layout', alone('app/teams/team+/layout')).then(
    () => undefined,
    (error) => error,
  )
  assert(failed instanceof RouteError && failed.status === 500 && failed.message === 'down', `failure ${failed}`)
})

test('a redirect of the server reaches every route as a redirect', async () => {
  const requests = stubFetch(200, { redirect: '/login', status: 302 })
  const source = collector(() => 'b1')
  const results = await Promise.all([source('/teams/x/one', 'app/teams/team+/layout', teamChain), source('/teams/x/one', 'app/teams/team+/one/route', teamChain)])
  for (const result of results)
    assert(result.status === 302 && result.location === '/login', `result ${JSON.stringify(result)}`)
  assert(requests.options[0]?.redirect === 'manual', `fetch follows redirects: ${JSON.stringify(requests.options[0])}`)
  // The loader turns it into a router redirect to the target.
  const thrown = await load({ source: async () => results[0], chain: () => [] }, 'app/teams/team+/layout', { pathname: '/teams/x/one', searchStr: '' }, false).then(
    () => undefined,
    (error) => error,
  )
  assert(isRedirect(thrown) && thrown.options.href === '/login', `loader threw ${thrown}`)
})

// browser stubs a window whose location counts reloads, with a sessionStorage that outlives
// resetReloads, as the storage of a tab outlives the reload of its page. 'blocked' makes the
// storage throw, as it does when the browser refuses site data, and 'forgets' takes what it is
// given and keeps nothing. name is what the app keeps in window.name.
function browser(storage: 'works' | 'blocked' | 'forgets' = 'works', name = '') {
  const items = new Map<string, string>()
  let reloads = 0
  const state = { reloads: () => reloads, items, listeners: {} as Record<string, (event: any) => void> }
  ;(globalThis as any).window = {
    name,
    location: { reload: () => reloads++, assign: () => {} },
    get sessionStorage() {
      if (storage === 'blocked') throw new DOMException('The operation is insecure.', 'SecurityError')
      return {
        getItem: (key: string) => items.get(key) ?? null,
        setItem: (key: string, value: string) => void (storage === 'works' && items.set(key, value)),
      }
    },
    addEventListener: (type: string, listener: (event: any) => void) => void (state.listeners[type] = listener),
  }
  resetReloads()
  return state
}

// settle lets the collector send its batch and handle the answer.
const settle = () => new Promise((resolve) => setTimeout(resolve, 20))

// standing reports how a promise stands after the answers came: 'pending', or what it settled as.
async function standing(promise: Promise<unknown>) {
  let result = 'pending'
  promise.then(
    () => (result = 'resolved'),
    (error) => (result = error instanceof RouteError ? `RouteError ${error.status}` : String(error)),
  )
  await settle()
  return result
}

test('a 409 reloads the current page once', async () => {
  const win = browser()
  stubFetch(409, { error: 'the application was updated' }, { 'X-Tifa-Build': 'new' })
  const source = collector(() => 'old')
  const hovered = source('/hovered', 'app/a/layout', alone('app/a/layout'))
  void source('/clicked', 'app/b/layout', alone('app/b/layout'))
  assert((await standing(hovered)) === 'pending', 'the loaders of the old build were answered')
  assert(win.reloads() === 1, `reloads ${win.reloads()}`)
  assert(win.items.get('tifa-reload:new') === '1', `storage ${[...win.items.keys()]}`)
})

test('the page reloaded for a build shows the error when that build disagrees again', async () => {
  const win = browser()
  stubFetch(409, { error: 'the application was updated' }, { 'X-Tifa-Build': 'new' })
  const source = collector(() => 'old')
  void source('/first', 'app/a/layout', alone('app/a/layout'))
  await settle()
  assert(win.reloads() === 1, `first reload ${win.reloads()}`)
  // The reload brought a page of the same old build, from a cache. Its memory is new, the tab's
  // storage is the same.
  resetReloads()
  const again = await standing(source('/second', 'app/a/layout', alone('app/a/layout')))
  assert(win.reloads() === 1 && again === 'RouteError 409', `second: reloads ${win.reloads()}, ${again}`)
  // Another build is another reason to reload.
  resetReloads()
  stubFetch(409, { error: 'the application was updated' }, { 'X-Tifa-Build': 'newer' })
  void source('/third', 'app/a/layout', alone('app/a/layout'))
  await settle()
  assert(win.reloads() === 2, `reload for another build ${win.reloads()}`)
})

test('a 409 without a build in it, from an old server, reloads once too', async () => {
  const win = browser()
  stubFetch(409, { error: 'the application was updated' })
  const source = collector(() => 'old')
  void source('/first', 'app/a/layout', alone('app/a/layout'))
  await settle()
  resetReloads()
  const again = await standing(source('/second', 'app/a/layout', alone('app/a/layout')))
  assert(win.reloads() === 1 && again === 'RouteError 409', `reloads ${win.reloads()}, ${again}`)
})

// A page that cannot keep a mark of its reload does not reload: the build may go on disagreeing,
// and each reload would bring back the page that disagrees. Every signal shows its failure.
test('with the storage blocked or forgetful a page never reloads, in any document', async () => {
  for (const storage of ['blocked', 'forgets'] as const) {
    const win = browser(storage)
    stubFetch(409, { error: 'the application was updated, reload the page' }, { 'X-Tifa-Build': 'new' })
    const source = collector(() => 'old')
    for (let document = 1; document <= 3; document++) {
      resetReloads() // the next document of the tab, which the visitor reloaded or the browser did
      const failed = await standing(source('/page', 'app/a/layout', alone('app/a/layout')))
      assert(failed === 'RouteError 409' && win.reloads() === 0, `${storage}, document ${document}: ${failed}, reloads ${win.reloads()}`)
    }
    watchChunks(() => 'b1')
    const chunk = preloadError(chunkMessage)
    win.listeners['vite:preloadError'](chunk)
    assert(!chunk.defaultPrevented && win.reloads() === 0, `${storage}: chunk prevented ${chunk.defaultPrevented}, reloads ${win.reloads()}`)
    assert(/reload the page/.test(chunk.payload.message) && chunk.payload.message.includes(chunkMessage), `${storage}: message ${chunk.payload.message}`)
  }
})

test('a build that keeps disagreeing reloads the page once in the first document and not in the next', async () => {
  const win = browser()
  stubFetch(409, { error: 'the application was updated, reload the page' }, { 'X-Tifa-Build': 'new' })
  const source = collector(() => 'old')
  for (let document = 1; document <= 3; document++) {
    resetReloads()
    const failed = await standing(source('/page', 'app/a/layout', alone('app/a/layout')))
    // The first document waits for its reload. The reloaded ones show the failure.
    assert(failed === (document === 1 ? 'pending' : 'RouteError 409') && win.reloads() === 1, `document ${document}: ${failed}, reloads ${win.reloads()}`)
  }
})

test('nothing outside sessionStorage keeps the first reload from happening', async () => {
  // window.name holds what an app or another page left there, the mark among it.
  const win = browser('works', 'tifa-reload:new tifa-reload:chunk:b1')
  stubFetch(409, { error: 'the application was updated' }, { 'X-Tifa-Build': 'new' })
  void collector(() => 'old')('/page', 'app/a/layout', alone('app/a/layout'))
  await settle()
  assert(win.reloads() === 1, `reloads ${win.reloads()}`)
  assert((globalThis as any).window.name === 'tifa-reload:new tifa-reload:chunk:b1', 'the name of the window was written')
  resetReloads()
  watchChunks(() => 'b1')
  const chunk = preloadError(chunkMessage)
  win.listeners['vite:preloadError'](chunk)
  assert(chunk.defaultPrevented && win.reloads() === 2, `chunk: prevented ${chunk.defaultPrevented}, reloads ${win.reloads()}`)
})

// vite:preloadError event: the one Vite sends when a dynamic import fails.
function preloadError(message: string) {
  return {
    payload: new TypeError(message),
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true
    },
  }
}
const chunkMessage = 'Failed to fetch dynamically imported module: http://tifa.test/assets/chunk-1.js'

test('a code chunk that fails to load reloads the page once for its build', async () => {
  const win = browser()
  watchChunks(() => 'b1')
  const first = preloadError(chunkMessage)
  win.listeners['vite:preloadError'](first)
  assert(first.defaultPrevented && win.reloads() === 1, `first: prevented ${first.defaultPrevented}, reloads ${win.reloads()}`)
  // Several chunks of one page fail together.
  const together = preloadError(chunkMessage)
  win.listeners['vite:preloadError'](together)
  assert(together.defaultPrevented && win.reloads() === 1, `together: prevented ${together.defaultPrevented}, reloads ${win.reloads()}`)
  // The reloaded page is the same build, so it throws the error instead.
  resetReloads()
  const second = preloadError(chunkMessage)
  win.listeners['vite:preloadError'](second)
  assert(!second.defaultPrevented && win.reloads() === 1, `second: prevented ${second.defaultPrevented}, reloads ${win.reloads()}`)
  assert(second.payload.message.includes(chunkMessage), `the message lost the URL: ${second.payload.message}`)
  // A page of another build has its own turn.
  resetReloads()
  watchChunks(() => 'b2')
  const other = preloadError(chunkMessage)
  win.listeners['vite:preloadError'](other)
  assert(other.defaultPrevented && win.reloads() === 2, `other build: prevented ${other.defaultPrevented}, reloads ${win.reloads()}`)
})

test('a chunk failure and a 409 on one page reload it once', async () => {
  const win = browser()
  watchChunks(() => 'b1')
  win.listeners['vite:preloadError'](preloadError(chunkMessage))
  stubFetch(409, { error: 'the application was updated' }, { 'X-Tifa-Build': 'new' })
  void collector(() => 'b1')('/x', 'app/a/layout', alone('app/a/layout'))
  await settle()
  assert(win.reloads() === 1, `reloads ${win.reloads()}`)
})

// The session of the request reaches the render as part of its input. The document says
// data-session="in" for someone signed in, "out" for someone who is not and nothing on a page that carries no session, begins its head with the script and
// the style Go wrote, and the page reads the session with useSession.
const sessionHead = '<script>/* HEAD_SCRIPT */</script><style>/* HEAD_STYLE */</style>'
const sessionInput = (session?: object) => ({ sessionKey: 'app_sid:v1', sessionHead, ...(session ? { session } : {}) })

test('a page for someone signed in says so on <html> and reads the session', async () => {
  const html = await render('/session', appData, undefined, undefined, undefined, sessionInput({ signedIn: true, data: { name: 'Ann' } }))
  assert(html.startsWith('<!DOCTYPE html><html data-session="in" lang="xx" data-theme="a&quot;b" class="h-full"><head><meta charset="utf-8">' + sessionHead + '<link'), `document start: ${html.slice(0, 200)}`)
  contains(html, 'SESSION <b>true</b>', 'NAME <b>Ann</b>', 'READY <b>true</b>', '<p class="menu" data-session="in">IN_ONLY</p>', '<p data-session="out">OUT_ONLY</p>')
  // The page carries the session and where the browser keeps it, for the hydration.
  contains(html, 'app_sid:v1', 'Ann')
  let dehydrated: Dehydrated | undefined
  const { calls } = await hydrateFrom(html, '/session', (d) => (dehydrated = d))
  assert(calls.length === 0, `hydration requested ${JSON.stringify(calls)}`)
  assert(JSON.stringify(dehydrated) === '{"buildId":"b1","sessionKey":"app_sid:v1","session":{"signedIn":true,"data":{"name":"Ann"}}}', `dehydrated ${JSON.stringify(dehydrated)}`)
})

test('a page for someone signed out says out on <html>', async () => {
  const html = await render('/session', appData, undefined, undefined, undefined, sessionInput({ signedIn: false }))
  assert(html.startsWith('<!DOCTYPE html><html data-session="out" lang="xx" data-theme="a&quot;b" class="h-full"><head>'), `document start: ${html.slice(0, 80)}`)
  contains(html, 'SESSION <b>false</b>', 'NAME <b>nobody</b>', 'READY <b>true</b>')
  lacks(html, 'data-session="in" lang')
})

test('a page that carries no session renders signed out and not ready', async () => {
  // What a page the server answers for every visitor alike looks like: the key and no session.
  const html = await render('/session', appData, undefined, undefined, undefined, sessionInput())
  lacks(html.slice(0, 40), 'data-session')
  contains(html, 'SESSION <b>false</b>', 'READY <b>false</b>')
  let dehydrated: Dehydrated | undefined
  await hydrateFrom(html, '/session', (d) => (dehydrated = d))
  assert(JSON.stringify(dehydrated) === '{"buildId":"b1","sessionKey":"app_sid:v1"}', `dehydrated ${JSON.stringify(dehydrated)}`)
})

test('useSession fails in an app render that has no session', async () => {
  const result = await renderResult('/session', appData)
  assert(result.html === undefined && /useSession needs a session/.test(result.error ?? ''), `result ${JSON.stringify(result).slice(0, 300)}`)
  // A page that does not read the session renders the same with or without one.
  contains(await render('/hello', appData), 'PAGE_LOWER')
  lacks(await render('/hello', appData), 'data-session')
})

test('the app cannot set data-session on <html>', async () => {
  for (const attrs of [{ 'data-session': 'in' }, { 'Data-Session': 'in' }])
    try {
      htmlAttributesOf([], attrs)
      assert(false, 'data-session was accepted')
    } catch (error) {
      assert(error instanceof Error && /data-session/.test(error.message), `error ${error}`)
    }
})

test('a root Layout that sets data-session stops the bundle as it loads, and says why', async () => {
  try {
    serve(routeTree, { lang: 'en', 'data-session': 'in' })
    assert(false, 'serve accepted data-session')
  } catch (error) {
    assert(error instanceof Error && /htmlAttributes cannot set data-session on <html>: tifa keeps it/.test(error.message), `error ${error}`)
  }
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
