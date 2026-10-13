// Run by TestGeneratedRouterBehaves in Node, against the route tree generated for this app.
import { createMemoryHistory } from '@tanstack/react-router'
import { routeTree } from './.tifa/routes'
import { createAppRouter } from './.tifa/runtime/router'
import { serve } from './.tifa/runtime/server'
import type { RouteResult } from './.tifa/runtime/data'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}
const ok = (data: unknown): RouteResult => ({ status: 200, data })
const missing: RouteResult = { status: 404 }

serve(routeTree)

// render runs one whole-page render through the SSR entry, as the Go VM pool does.
async function render(url: string, data: Record<string, RouteResult>): Promise<string> {
  globalThis.renderResult = undefined
  globalThis.startRender(JSON.stringify({ url, buildId: 'b1', data }))
  for (let i = 0; i < 1000 && globalThis.renderResult === undefined; i++)
    await new Promise((resolve) => setTimeout(resolve, 0))
  const result = JSON.parse(globalThis.renderResult ?? '{"error":"render did not finish"}')
  if (result.error) throw new Error(`render ${url}: ${result.error}`)
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

test('literal segments match the URLs ServeMux matches', async () => {
  contains(await render('/a', appData), 'PAGE_ENCODED')
  contains(await render('/x%22y', appData), 'PAGE_QUOTE')
  contains(await render('/x%5Cy', appData), 'PAGE_BACKSLASH')
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
