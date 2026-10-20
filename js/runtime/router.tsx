import {
  createMemoryHistory,
  createRouter,
  createSerializationAdapter,
  notFound,
  redirect,
  rootRouteId,
  type AnyRoute,
  type ErrorComponentProps,
  type RouterHistory,
} from '@tanstack/react-router'
import { goJSON, RouteError, type Source } from './data'

// RouterContext is the context of the root route. Loaders read their data through source.
export interface RouterContext {
  source: Source
}

interface Dehydrated {
  buildId: string
}

export interface RouterOptions {
  // The build ID of a page the server renders.
  buildId?: string
  // The URL of a server render. Without it the router runs in the browser.
  serverURL?: string
  // Receives the build ID of the page when the browser hydrates it.
  onHydrate?: (buildId: string) => void
  // The history of a browser router, the browser's own by default. Tests pass a memory history.
  history?: RouterHistory
}

export function createAppRouter<TRouteTree extends AnyRoute>(
  routeTree: TRouteTree,
  source: Source,
  options: RouterOptions,
) {
  const { buildId, serverURL, onHydrate } = options
  const router = createRouter({
    routeTree,
    context: { source },
    isServer: serverURL !== undefined,
    history: serverURL ? createMemoryHistory({ initialEntries: [serverURL] }) : options.history,
    // ServeMux matches paths case-sensitively and reads query values as strings.
    caseSensitive: true,
    parseSearch,
    stringifySearch,
    // A URL no route matches shows the root's NotFound at once, without loading any route.
    notFoundMode: 'root',
    defaultPreload: 'intent',
    // Data goes stale only through invalidate, after a write. The reload then finishes before
    // invalidate or the navigation resolves, so a handler function resolves once the page
    // shows the new data, rather than showing the old data and loading in the background.
    defaultStaleReloadMode: 'blocking',
    scrollRestoration: true,
    dehydrate: (): Dehydrated => {
      if (buildId === undefined) throw new Error('a server render needs the build ID')
      return { buildId }
    },
    hydrate: (dehydrated: Dehydrated) => {
      if (onHydrate) onHydrate(dehydrated.buildId)
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

// load is the loader of every route with a Go Load: it asks the source for the route's result
// at the URL being loaded. 404 becomes notFound(), which the router shows with the first
// notFoundComponent from the route up. A redirect becomes redirect(), other failures a
// RouteError for the Error components.
export async function load<T>(
  context: RouterContext,
  id: string,
  location: { pathname: string; searchStr: string },
): Promise<T> {
  const result = await context.source(location.pathname + location.searchStr, id)
  if (result.status === 200) return result.data as T
  if (result.status === 404) throw notFound()
  if (result.location !== undefined) throw redirect({ href: result.location, statusCode: result.status })
  throw new RouteError(result.status, result.error ?? `route ${id} failed with status ${result.status}`)
}

// unmatched is the beforeLoad of the index route that tifa build adds below a Layout without a
// page in its own directory. Go matches no route at that URL, so the router shows the root's
// NotFound before any loader runs, as it does for every URL no route matches.
export function unmatched(): never {
  throw notFound({ routeId: rootRouteId })
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

export function DefaultNotFound() {
  return (
    <main>
      <h1>Page not found</h1>
    </main>
  )
}

export function DefaultError({ error }: ErrorComponentProps) {
  return (
    <main>
      <h1>Something went wrong</h1>
      <p>{error instanceof RouteError ? error.message : 'Reload the page to try again.'}</p>
    </main>
  )
}
