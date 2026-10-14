// Run by TestGeneratedRouterBehaves in Node, against the route tree generated for this app.
import { createMemoryHistory, isRedirect } from '@tanstack/react-router'
import { routeTree } from './.tifa/routes'
import { createAppRouter, load } from './.tifa/runtime/router'
import { serve } from './.tifa/runtime/server'
import { collector, RouteError, type RouteResult } from './.tifa/runtime/data'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}
const ok = (data: unknown): RouteResult => ({ status: 200, data })
const missing: RouteResult = { status: 404 }

serve(routeTree)

// renderResult runs one whole-page render through the SSR entry, as the Go VM pool does.
async function renderResult(url: string, data: Record<string, RouteResult>) {
  globalThis.renderResult = undefined
  globalThis.startRender(JSON.stringify({ url, buildId: 'b1', data }))
  for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++)
    await new Promise((resolve) => setTimeout(resolve, 0))
  return JSON.parse(globalThis.renderResult ?? '{"error":"render did not finish"}') as {
    html?: string
    redirect?: { location: string; status: number }
    error?: string
  }
}

async function render(url: string, data: Record<string, RouteResult>): Promise<string> {
  const result = await renderResult(url, data)
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

const appData = { app: ok({ name: 'router' }) }

test('Layouts with the same prefix are distinct routes', async () => {
  const { calls } = await client('/shared/three')
  const ids = calls.map(([, id]) => id).sort()
  assert(JSON.stringify(ids) === '["app","app/b"]', `loaded ${ids}`)
  contains(await render('/shared/one', { ...appData, 'app/a': ok('a') }), 'LAYOUT_A', 'PAGE_A_ONE')
  contains(await render('/shared/four', { ...appData, 'app/b': ok('b') }), 'LAYOUT_B', 'PAGE_B_FOUR')
})

test('query values stay the strings Go reads', async () => {
  const { router, calls } = await client('/q?q=1.0')
  assert(router.state.location.href === '/q?q=1.0', `href ${router.state.location.href}`)
  assert(calls.some(([u, id]) => id === 'app/query' && u === '/q?q=1.0'), `calls ${JSON.stringify(calls)}`)
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
  contains(await render('/q?q=1.0', { ...appData, 'app/query': ok('1.0') }), 'QUERY <!-- -->1.0')
})

test('paths match case-sensitively, as ServeMux does', async () => {
  contains(await render('/HELLO', appData), 'PAGE_UPPER')
  contains(await render('/hello', appData), 'PAGE_LOWER')
})

test('literal segments match the URLs ServeMux matches', async () => {
  contains(await render('/a', appData), 'PAGE_ENCODED')
  // The router spells /%61 as /a and redirects there, Go then serves the redirect.
  const redirected = await renderResult('/%61', appData)
  assert(redirected.redirect?.location === '/a', `render /%61: ${JSON.stringify(redirected)}`)
  contains(await render('/x%22y', appData), 'PAGE_QUOTE')
  contains(await render('/x%5Cy', appData), 'PAGE_BACKSLASH')
})

test('a component that throws fails the render', async () => {
  const result = await renderResult('/throws', appData)
  assert(result.html === undefined && result.error !== undefined, `render /throws: ${JSON.stringify(result)}`)
})

test('a failed root Load keeps the document', async () => {
  const failed = await render('/hello', { app: { status: 500, error: 'boom' } })
  contains(failed, '<html lang="xx">', 'boom', 'src="/assets/app.js"', 'href="/assets/app.css"')
  const notFound = await render('/hello', { app: missing })
  contains(notFound, '<html lang="xx">', 'APP_404', 'src="/assets/app.js"')
})

test("a Layout's 404 is shown from the level above, inside the ancestors", async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/team': missing, 'app/team/one': ok('one') })
  contains(html, 'APP_NAV', 'APP_404')
  lacks(html, 'TEAM_404', 'TEAM_ONE')
})

test("a page's 404 is shown inside its Layouts", async () => {
  const html = await render('/teams/x/one', { ...appData, 'app/team': ok('x'), 'app/team/one': missing })
  contains(html, 'APP_NAV', 'TEAM <!-- -->x', 'TEAM_404')
  lacks(html, 'TEAM_ONE')
})

test("an unmatched URL shows the root Layout's NotFound without asking for data", async () => {
  const { calls } = await client('/teams/x/missing')
  assert(calls.length === 0, `requested ${JSON.stringify(calls)}`)
  contains(await render('/teams/x/missing', {}), '<html lang="xx">', 'APP_404', 'src="/assets/app.js"')
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
  stubFetch(404, { 'app/team': ok('x'), 'app/team/one': missing })
  const source = collector(() => 'b1')
  const [team, one] = await Promise.all([source('/teams/x/one', 'app/team'), source('/teams/x/one', 'app/team/one')])
  assert(team.status === 200 && team.data === 'x', `team ${JSON.stringify(team)}`)
  assert(one.status === 404, `one ${JSON.stringify(one)}`)

  stubFetch(500, { error: 'down' })
  const failed = await source('/teams/x/one', 'app/team').then(
    () => undefined,
    (error) => error,
  )
  assert(failed instanceof RouteError && failed.status === 500 && failed.message === 'down', `failure ${failed}`)
})

test('a redirect of the server reaches every route as a redirect', async () => {
  const requests = stubFetch(200, { redirect: '/login', status: 302 })
  const source = collector(() => 'b1')
  const results = await Promise.all([source('/teams/x/one', 'app/team'), source('/teams/x/one', 'app/team/one')])
  for (const result of results)
    assert(result.status === 302 && result.location === '/login', `result ${JSON.stringify(result)}`)
  assert(requests.options[0]?.redirect === 'manual', `fetch follows redirects: ${JSON.stringify(requests.options[0])}`)
  // The loader turns it into a router redirect to the target.
  const thrown = await load({ source: async () => results[0] }, 'app/team', { pathname: '/teams/x/one', searchStr: '' }).then(
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
  void source('/hovered', 'app/a')
  void source('/clicked', 'app/b')
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
