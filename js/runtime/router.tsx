import type { ComponentType } from 'react'
import {
  createMemoryHistory,
  createRouter,
  createSerializationAdapter,
  redirect,
  type AnyRoute,
  type AnyRouter,
  type ErrorComponentProps,
  type RouterHistory,
} from '@tanstack/react-router'
import { goJSON, RouteError, type Source } from './data'
import type { SessionEnvelope } from './session'

// RouterContext is the context of the root route. Loaders read their data through source, and
// chain says which Loads a path has: the IDs of the routes with a Go Load that the router matches
// there, from the root Layout down, which is what a data request counts its digits against.
export interface RouterContext {
  source: Source
  chain: (pathname: string) => string[]
}

// Dehydrated is what the page carries from the server render to the browser besides the data:
// the build, and for an app with sessions the key of the copy and the session of the request.
export interface Dehydrated {
  buildId: string
  sessionKey?: string
  session?: SessionEnvelope
}

export interface RouterOptions {
  // The build ID of a page the server renders.
  buildId?: string
  // The URL of a server render. Without it the router runs in the browser.
  serverURL?: string
  // The key of the browser's copy of the session, and the session of the request, which the page
  // carries to the browser. An app without sessions has neither.
  sessionKey?: string
  session?: SessionEnvelope
  // Receives what the page carried when the browser hydrates it.
  onHydrate?: (dehydrated: Dehydrated) => void
  // The history of a browser router, the browser's own by default. Tests pass a memory history.
  history?: RouterHistory
}

export function createAppRouter<TRouteTree extends AnyRoute>(
  routeTree: TRouteTree,
  source: Source,
  options: RouterOptions,
) {
  const { buildId, serverURL, onHydrate, sessionKey, session } = options
  const context: RouterContext = {
    source,
    chain: (pathname) => loadsAt(router, pathname),
  }
  const router = createRouter({
    routeTree,
    context,
    isServer: serverURL !== undefined,
    history: serverURL ? createMemoryHistory({ initialEntries: [serverURL] }) : options.history,
    // ServeMux matches paths case-sensitively and reads query values as strings.
    caseSensitive: true,
    parseSearch,
    stringifySearch,
    // A URL no route matches shows the notFoundComponent of the deepest Layout it matches in
    // that Layout's Outlet, after the Layouts above it have loaded. Go finds the same Layout
    // for that URL (notFoundAt in server/notfound.go) and runs the same Layout functions.
    notFoundMode: 'fuzzy',
    defaultPreload: 'intent',
    scrollRestoration: true,
    dehydrate: (): Dehydrated => {
      if (buildId === undefined) throw new Error('a server render needs the build ID')
      const out: Dehydrated = { buildId }
      if (sessionKey !== undefined) out.sessionKey = sessionKey
      if (session !== undefined) out.session = session
      return out
    },
    hydrate: (dehydrated: Dehydrated) => {
      if (onHydrate) onHydrate(dehydrated)
    },
  })
  // The constructor options leave serialization adapters to TanStack Start, the router reads
  // them from its options when it dehydrates and hydrates. Setting them on a router of its own
  // is not documented (TanStack/router#7796), so the router tests check that loader data still
  // goes through loaderDataAdapter.
  router.options.serializationAdapters = [loaderDataAdapter, routeErrorAdapter]
  return router
}

// loaderDataAdapter writes the data of a Load into the page as the JSON text Go sent it in, read
// back with JSON.parse in the browser, so the browser reads what the render read (see
// InjectedResult in data.ts). The default serializer walks every value and escapes strings one
// character at a time, which takes several times as long in the VM for the same data. Other
// values the router writes, such as errors and single values of a Load, still go through the
// default serializer.
const loaderDataAdapter = createSerializationAdapter({
  key: 'tifa.data',
  test: (value): value is object => typeof value === 'object' && value !== null && goJSON.has(value),
  toSerializable: (value: object) => goJSON.get(value)!,
  fromSerializable: (text: string) => JSON.parse(text) as object,
})

// routeErrorAdapter carries a RouteError from the server render to the browser as a
// RouteError with its status. The default serializer would make it a plain Error, and the
// Error components would show something else after hydration.
const routeErrorAdapter = createSerializationAdapter({
  key: 'tifa.RouteError',
  test: (value): value is RouteError => value instanceof RouteError,
  toSerializable: (error: RouteError) => ({ status: error.status, message: error.message }),
  fromSerializable: (value: { status: number; message: string }) => new RouteError(value.status, value.message),
})

// A redirect a Load asked for carries this option, so that the server render can tell it from the
// one the router throws itself for a URL it spells differently (see canonicalStatus in server.tsx).
export const askedByLoad = 'tifaAskedByLoad'

// loadsAt lists the IDs of the Loads the router runs for pathname, from the root Layout down. A URL
// the router does not match whole shows a 404 inside one Layout, as it decides in
// matchRoutesInternal (the deepest matched route with a notFoundComponent, else the deepest with
// children), and only the Layouts down to that one load. Go finds the same Layout for that URL
// (notFoundAt in server/notfound.go) and runs the Loads of its chain.
function loadsAt(router: AnyRouter, pathname: string): string[] {
  const [matched, params, found] = router.getMatchedRoutes(pathname)
  const missing = found ? found.path !== '/' && params['**'] : pathname.replace(/\/+$/, '') !== ''
  let routes = matched
  if (missing) {
    const last = (keep: (route: AnyRoute) => unknown) => {
      for (let i = matched.length - 1; i >= 0; i--) if (keep(matched[i])) return i
      return -1
    }
    const end = last((route) => route.options.notFoundComponent)
    routes = matched.slice(0, Math.max(end >= 0 ? end : last((route) => route.children), 0) + 1)
  }
  return routes
    .map((route) => (route.options.staticData as Partial<LoadData> | undefined)?.load)
    .filter((id): id is string => id !== undefined)
}

// LoadData is the staticData of a route with a Go Load: the ID of that Load.
interface LoadData {
  load: string
}

// load is the loader of every route with a Go Load: it asks the source for the route's result
// at the URL being loaded. preload is true when the router loads it ahead of a navigation. A
// redirect becomes redirect(), a failure, 404 included, a RouteError for the Error components.
export async function load<T>(
  context: RouterContext,
  id: string,
  location: { pathname: string; searchStr: string },
  preload: boolean,
): Promise<T> {
  const result = await context.source(location.pathname + location.searchStr, id, {
    // Only a request from the browser needs it, a server render has its data injected.
    chain: () => context.chain(location.pathname),
    preload,
  })
  if (result.status === 200) return result.data as T
  if (result.location !== undefined) {
    throw redirect({ href: result.location, statusCode: result.status, [askedByLoad]: true } as Parameters<typeof redirect>[0])
  }
  throw new RouteError(result.status, result.error ?? `route ${id} failed with status ${result.status}`)
}

// notFoundMessage is the public message of a 404, the one Go gives tifa.ErrNotFound.
const notFoundMessage = 'Not Found'

// unmatched is the loader of the index route that tifa build adds below a Layout without a page
// at its own URL. Go matches no route there and answers with a 404 inside the Layout, so the
// route fails with one, which its Error shows in the Layout's Outlet.
export function unmatched(): never {
  throw new RouteError(404, notFoundMessage)
}

// outside makes the params.parse of the route that tifa build adds for the rest of a URL below
// the root Layout. The router matches a URL that no route below the root Layout starts with the
// root route alone, without the root Layout, so this route takes such a URL and fails with a 404
// inside the root Layout. It turns down every other URL, which the router then matches in part,
// with the 404 inside the deepest Layout it matches. A prefix holds the segments of a path that a
// URL may start with, null for a parameter, which takes any segment but an empty one.
export function outside(prefixes: ReadonlyArray<ReadonlyArray<string | null>>) {
  return (params: { _splat?: string }) => {
    const parts = (params._splat ?? '').split('/')
    const starts = prefixes.some(
      (prefix) => prefix.length <= parts.length && prefix.every((segment, i) => (segment === null ? parts[i] !== '' : segment === parts[i])),
    )
    // The router leaves out a match whose parse returns false.
    return starts ? (false as never) : {}
  }
}

// missing makes the notFoundComponent of a Layout from the Error it shows: a URL that matches
// nothing below the Layout reaches that Error as a RouteError with status 404. preload loads a
// lazy Error with the route, so hydration does not wait for it.
export function missing(Error: ComponentType<ErrorComponentProps> & { preload?: () => Promise<void> | undefined }) {
  const error = new RouteError(404, notFoundMessage)
  const NotFound = () => <Error error={error} reset={() => {}} />
  NotFound.preload = Error.preload
  return NotFound
}

// parseSearch reads the query the way c.Query does: every value is a string, and a key given
// more than once holds the array of its values. The object has no prototype, so a key such as
// __proto__ is a parameter like any other.
export function parseSearch(searchStr: string): Record<string, string | string[]> {
  const search: Record<string, string | string[]> = Object.create(null)
  for (const [key, value] of new URLSearchParams(searchStr)) {
    const previous = search[key]
    if (previous === undefined) search[key] = value
    else if (Array.isArray(previous)) previous.push(value)
    else search[key] = [previous, value]
  }
  return search
}

// stringifySearch writes a query parseSearch reads back as the same values, spelled the way
// browsers and forms send it, so the router keeps the URL of a request as it is: a space is +,
// and only the characters that would change the meaning, or that browsers escape, are escaped.
export function stringifySearch(search: Record<string, unknown>): string {
  const parts: string[] = []
  for (const key in search) {
    const value = search[key]
    if (value === undefined) continue
    for (const item of Array.isArray(value) ? value : [value])
      parts.push(encodeQueryPart(key) + '=' + encodeQueryPart(String(item)))
  }
  return parts.length > 0 ? '?' + parts.join('&') : ''
}

// The characters encodeURIComponent escapes that browsers send as they are and Go's
// url.ParseQuery reads back as the same character. A semicolon is not one of them: ParseQuery
// drops a parameter holding one, so it stays %3B. So do & = + % #, which encodeURIComponent
// escapes and this list leaves escaped.
const keptInQuery = /%(24|2C|2F|3A|3F|40|5B|5C|5D|5E|60|7B|7C|7D)/g

function encodeQueryPart(text: string) {
  return encodeURIComponent(text)
    .replace(keptInQuery, (escape) => decodeURIComponent(escape))
    .replace(/'/g, '%27')
    .replace(/%20/g, '+')
}

// pick keeps the query parameters a Load reads, for loaderDeps and remountDeps. Other
// parameters then neither refetch nor remount. Like parseSearch it builds an object without a
// prototype, so __proto__ is kept as a value instead of being taken as the prototype.
export function pick(search: object, keys: string[]): Record<string, unknown> {
  const values = search as Record<string, unknown>
  const picked: Record<string, unknown> = Object.create(null)
  for (const key of keys) if (Object.hasOwn(values, key)) picked[key] = values[key]
  return picked
}

// DefaultError is the Error of a route with no Error of its own or above it: the 404 page for a
// RouteError with status 404, and an error page for anything else.
export function DefaultError({ error }: ErrorComponentProps) {
  if (error instanceof RouteError && error.status === 404)
    return (
      <main>
        <h1>Page not found</h1>
      </main>
    )
  return (
    <main>
      <h1>Something went wrong</h1>
      <p>{error instanceof RouteError ? error.message : 'Reload the page to try again.'}</p>
    </main>
  )
}
