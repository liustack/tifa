// Run by TestTanStackMembersStay in Node, against the route tree generated for this app. It builds
// a server router and a browser router the way the framework does and checks that every member of
// TanStack Router that tifa uses, and that TanStack does not document for apps, is still there. A
// version of TanStack that moves one makes this fail with what in tifa depends on it, long before a
// page goes blank in a browser.
import { createMemoryHistory } from '@tanstack/react-router'
import { hydrate } from '@tanstack/react-router/ssr/client'
import { attachRouterServerSsrUtils } from '@tanstack/react-router/ssr/server'
import { routeTree } from './.tifa/routes'
import { createAppRouter } from './.tifa/runtime/router'
import { injected, RouteError, type Injected, type RouteResult } from './.tifa/runtime/data'

type Anything = any

// Each check names what breaks in tifa and the files that use the member.
const failures: string[] = []
function check(member: string, breaks: string, files: string, ok: () => unknown) {
  let found = false
  let why = ''
  try {
    found = Boolean(ok())
  } catch (error) {
    why = ` (${error instanceof Error ? error.message : error})`
  }
  if (!found) failures.push(`${member} is gone${why}. Breaks: ${breaks}. Used in: ${files}`)
}
const fn = (value: unknown) => typeof value === 'function'

const url = '/hello'
const data: Injected['data'] = {
  'app/layout': { status: 200, json: '{"name":"router"}' },
}
const source = injected({ url, buildId: 'b1', data })

// The server router of a render, made as startRender in server.tsx makes it.
const server: Anything = createAppRouter(routeTree, source, { buildId: 'b1', serverURL: url })
attachRouterServerSsrUtils({ router: server, manifest: undefined })

check(
  'router.stores.setMatches',
  'a render cannot date the matches (undated) or show an error at a level (placed)',
  'js/runtime/server.tsx',
  () => fn(server.stores.setMatches),
)
check(
  'router.getMatchedRoutes',
  'routesAt cannot tell which routes a URL matches, so Go and the router disagree on the chain of Loads and the language of a route',
  'js/runtime/router.tsx, prefix.ts',
  () => fn(server.getMatchedRoutes) && Array.isArray(server.getMatchedRoutes('/hello')[0]),
)
check(
  'router.parseLocation',
  'links and handler calls cannot read the plain path of a public URL',
  'js/runtime/html.tsx, call.ts',
  () => fn(server.parseLocation) && server.parseLocation({ href: '/hello', pathname: '/hello', search: '', hash: '', state: {} }).pathname === '/hello',
)
check(
  'location.publicHref',
  'a data request of a site with languages loses the language prefix, and a navigation to another language does not leave the document',
  'js/runtime/router.tsx, alternates.ts',
  () => server.state.location.publicHref === '/hello',
)
for (const name of ['dehydrate', 'takeInitialHydrationScriptTags', 'setRenderFinished', 'cleanup']) {
  check(
    `router.serverSsr.${name}`,
    'the server cannot write the page data, and no page reaches the browser',
    'js/runtime/server.tsx',
    () => fn(server.serverSsr?.[name]),
  )
}

// The render itself: load, dehydrate, take the scripts.
await server.load()
check(
  'router._serverResult',
  'the server render cannot tell a redirect of the router from a page',
  'js/runtime/server.tsx',
  () => server._serverResult?.type === 'render',
)
const redirecting: Anything = createAppRouter(routeTree, injected({ url, buildId: 'b1', data: { 'app/layout': { status: 302, location: '/login' } } }), { buildId: 'b1', serverURL: url })
attachRouterServerSsrUtils({ router: redirecting, manifest: undefined })
await redirecting.load()
check(
  'router._serverResult of a redirect (type, redirect.headers)',
  'a redirect of a Load is not answered as a redirect',
  'js/runtime/server.tsx',
  () => redirecting._serverResult?.type === 'redirect' && redirecting._serverResult.redirect.headers.get('Location') === '/login',
)
redirecting.serverSsr.cleanup()

let scripts: Array<{ attrs?: Record<string, unknown>; children?: string }> = []
{
  await server.serverSsr.dehydrate()
  const taken = server.serverSsr.takeInitialHydrationScriptTags()
  server.serverSsr.setRenderFinished()
  scripts = taken ? [...taken.before, taken.boundary] : []
  server.serverSsr.cleanup()
}
const page = scripts.map((tag) => tag.children ?? '').join('\n')
check(
  'the hydration scripts of takeInitialHydrationScriptTags',
  'the page carries no data for the browser',
  'js/runtime/server.tsx',
  () => page.length > 0,
)
check(
  'router.options.serializationAdapters (written on the server)',
  'the data of a Load is written with the default serializer, not as the JSON text Go sent, and RouteError turns into a plain Error',
  'js/runtime/router.tsx',
  () => page.includes('tifa.data'),
)

// The browser reads the page as a browser does.
const g = globalThis as Anything
const saved = { window: g.window, self: g.self, document: g.document }
g.window = g.self = globalThis
try {
  for (const code of scripts.map((tag) => tag.children ?? '')) {
    if (code.trim() === '') continue
    g.document = { currentScript: { previousElementSibling: null, remove() {} }, querySelector: () => null }
    new Function(code)()
  }
  check(
    'window.$_TSR.h',
    'the browser cannot tell the page the router took over, and the first navigation waits',
    'js/runtime/browser.tsx',
    () => fn(g.window.$_TSR.h),
  )
  const browser: Anything = createAppRouter(routeTree, async (_u, id): Promise<RouteResult> => ({ status: 200, data: id }), {
    history: createMemoryHistory({ initialEntries: [url] }),
  })
  const hydrated = async (router: Anything, what: string) => {
    try {
      await hydrate(router)
    } catch (error) {
      failures.push("hydrating the browser router failed after " + what + ": " + (error instanceof Error ? error.message : error) + ". Breaks: the browser cannot take over the page. Used in: js/runtime/browser.tsx")
    }
  }
  await hydrated(browser, "the page data")
  check(
    'router.options.serializationAdapters (read in the browser)',
    'the data of the page is read back as the serialized form, and every page with object data stays blank',
    'js/runtime/router.tsx',
    () => browser.state.matches.some((m: Anything) => m.loaderData?.name === 'router'),
  )
  // A RouteError goes through its own adapter: without it the browser shows a plain Error.
  const failed: Anything = createAppRouter(routeTree, injected({ url, buildId: 'b1', data: { 'app/layout': { status: 403, error: 'no' } } }), { buildId: 'b1', serverURL: url })
  attachRouterServerSsrUtils({ router: failed, manifest: undefined })
  await failed.load()
  await failed.serverSsr.dehydrate()
  const taken = failed.serverSsr.takeInitialHydrationScriptTags()
  failed.serverSsr.setRenderFinished()
  failed.serverSsr.cleanup()
  g.$_TSR = undefined
  for (const tag of taken ? [...taken.before, taken.boundary] : []) {
    g.document = { currentScript: { previousElementSibling: null, remove() {} }, querySelector: () => null }
    new Function(tag.children ?? '')()
  }
  const again: Anything = createAppRouter(routeTree, async (_u, id): Promise<RouteResult> => ({ status: 200, data: id }), {
    history: createMemoryHistory({ initialEntries: [url] }),
  })
  await hydrated(again, "a failed Load")
  const error = again.state.matches[1]?.error
  check(
    'router.options.serializationAdapters (RouteError read in the browser)',
    'an error shown after hydration is a plain Error with no status',
    'js/runtime/router.tsx',
    () => error instanceof RouteError && error.status === 403,
  )
} finally {
  Object.assign(g, saved)
}

if (failures.length > 0) throw new Error(`TanStack Router changed under tifa:\n- ${failures.join('\n- ')}`)
console.log('ok   every member of TanStack Router that tifa relies on is there')
