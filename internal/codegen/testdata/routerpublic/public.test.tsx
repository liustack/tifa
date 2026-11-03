// Run by TestGeneratedRouterLeavesPublicFiles in Node, against the route tree generated for this
// app: a parameter folder at the root of app/, so /favicon.svg also matches /{lang}.
import { createMemoryHistory, isRedirect } from '@tanstack/react-router'
import { routeTree } from './.tifa/routes'
import { createAppRouter, isPage } from './.tifa/runtime/router'
import type { RouteResult } from './.tifa/runtime/data'

const tests: Array<[string, () => Promise<void>]> = []
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn])

function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message)
}

// client loads url on a browser-side router whose source records each call. The window records
// the documents the browser is sent to.
async function client(url: string) {
  const calls: string[] = []
  const documents: string[] = []
  const location = {
    set href(value: string) {
      documents.push(value)
    },
    replace(value: string) {
      documents.push(value)
    },
  }
  ;(globalThis as { window?: unknown }).window = { location }
  const router = createAppRouter(
    routeTree,
    async (u, id): Promise<RouteResult> => {
      calls.push(`${id} ${u}`)
      return { status: 200, data: id }
    },
    { history: createMemoryHistory({ initialEntries: [url] }) },
  )
  await router.load()
  calls.length = 0
  return { router, calls, documents }
}

test('a path of the public folder, /assets/ or /__tifa/ is no page', async () => {
  const { router } = await client('/en')
  for (const path of ['/favicon.svg', '/img/logo.png', '/assets/app.js', '/__tifa/dev/events'])
    assert(!isPage(router, path), `${path} is a page`)
})

test('pages are pages, and so is a path no file takes', async () => {
  const { router } = await client('/en')
  for (const path of ['/en', '/about', '/fr', '/img'])
    assert(isPage(router, path), `${path} is no page`)
})

test('an API route, a URL below a page and the root are no pages', async () => {
  const { router } = await client('/en')
  for (const path of ["/api/ping", "/en/missing", "/"])
    assert(!isPage(router, path), `${path} is a page`)
})

// The browser follows the redirect with a document load in router.navigate, which Node's build of
// TanStack does not run (test/e2e checks a click on a Link). Here the root route must ask for
// that load and the pages below it must not be asked for data.
test('navigating to a public file asks for a document load and for no data', async () => {
  const { router, calls } = await client('/en')
  router.history.push('/favicon.svg')
  await router.load()
  assert(calls.length === 0, `asked for ${JSON.stringify(calls)}`)
  const beforeLoad = (routeTree.options as { beforeLoad: (a: unknown) => void }).beforeLoad
  let thrown: unknown
  try {
    beforeLoad({ location: { pathname: '/favicon.svg', href: '/favicon.svg?v=1' } })
  } catch (error) {
    thrown = error
  }
  const options = (thrown as { options?: { href?: string; reloadDocument?: boolean } } | undefined)?.options
  assert(isRedirect(thrown) && options?.reloadDocument === true && options.href === '/favicon.svg?v=1', `threw ${JSON.stringify(options)}`)
  beforeLoad({ location: { pathname: '/about', href: '/about' } })
})


test('navigating to a page still asks for its data', async () => {
  const { router, calls, documents } = await client('/en')
  router.history.push('/about')
  await router.load()
  assert(documents.length === 0, `documents ${JSON.stringify(documents)}`)
  assert(calls.some((call) => call.endsWith(' /about')), `asked for ${JSON.stringify(calls)}`)
  assert(router.state.location.pathname === '/about', `at ${router.state.location.pathname}`)
})

test('preloading a public file asks for no data and loads no document', async () => {
  const { router, calls, documents } = await client('/en')
  await router.preloadRoute({ to: '/favicon.svg' as never })
  assert(calls.length === 0, `asked for ${JSON.stringify(calls)}`)
  assert(documents.length === 0, `documents ${JSON.stringify(documents)}`)
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
