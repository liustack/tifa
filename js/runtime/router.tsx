import {
  createMemoryHistory,
  createRouter,
  createSerializationAdapter,
  HeadContent,
  notFound,
  redirect,
  Scripts,
  type AnyRoute,
  type ErrorComponentProps,
  type RouterHistory,
} from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { RouteError, type Assets, type Source } from './data'

// RouterContext is the context of the root route. Loaders read their data through source.
export interface RouterContext {
  source: Source
}

interface Dehydrated {
  buildId: string
  assets: Assets
}

export interface RouterOptions {
  // The build ID and assets of a page the server renders, or of a page rendered without SSR.
  buildId?: string
  assets?: Assets
  // The URL of a server render. Without it the router runs in the browser.
  serverURL?: string
  // Receives the build ID of the page when the browser hydrates it.
  onHydrate?: (buildId: string) => void
  // The history of a browser router, the browser's own by default. Tests pass a memory history.
  history?: RouterHistory
}

// The assets of the current page. The root route's head and scripts render them, on the server
// from the render options and in the browser from the dehydrated state, so both render the same.
let pageAssets: Assets | undefined

export function createAppRouter<TRouteTree extends AnyRoute>(
  routeTree: TRouteTree,
  source: Source,
  options: RouterOptions,
) {
  const { buildId, serverURL, onHydrate } = options
  if (options.assets) pageAssets = options.assets
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
    scrollRestoration: true,
    dehydrate: (): Dehydrated => {
      if (buildId === undefined || pageAssets === undefined)
        throw new Error('a server render needs the build ID and the assets')
      return { buildId, assets: pageAssets }
    },
    hydrate: (dehydrated: Dehydrated) => {
      pageAssets = dehydrated.assets
      if (onHydrate) onHydrate(dehydrated.buildId)
    },
  })
  // The constructor options leave serialization adapters to TanStack Start, the router reads
  // them from its options when it dehydrates and hydrates.
  router.options.serializationAdapters = [routeErrorAdapter]
  return router
}

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
// at the URL being loaded. 404 becomes notFound(), shown by the closest NotFound from
// notFoundFrom up (a Layout passes its parent, a page itself). A redirect becomes redirect(),
// other failures a RouteError for the Error components.
export async function load<T>(
  context: RouterContext,
  id: string,
  location: { pathname: string; searchStr: string },
  notFoundFrom?: string,
): Promise<T> {
  const result = await context.source(location.pathname + location.searchStr, id)
  if (result.status === 200) return result.data as T
  if (result.status === 404) throw notFound(notFoundFrom ? { routeId: notFoundFrom as never } : {})
  if (result.location !== undefined) throw redirect({ href: result.location, statusCode: result.status })
  throw new RouteError(result.status, result.error ?? `route ${id} failed with status ${result.status}`)
}

type HeadResult = { links?: Array<Record<string, string>>; [key: string]: unknown } | undefined

// rootHead adds the stylesheet of the browser bundle to the head of the root route.
export function rootHead(head?: (ctx: any) => HeadResult | Promise<HeadResult>) {
  return (ctx: any) => {
    const add = (result: HeadResult) => {
      const style = pageAssets?.style
      const links = style ? [{ rel: 'stylesheet', href: style }] : []
      return { ...result, links: [...links, ...(result?.links ?? [])] }
    }
    const own = head ? head(ctx) : undefined
    return own instanceof Promise ? own.then(add) : add(own)
  }
}

// rootScripts loads the browser bundle from the root route.
export function rootScripts() {
  return pageAssets ? [{ type: 'module', src: pageAssets.script }] : []
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

// DefaultDocument is the document around every page when app/layout.tsx exports no Document.
// The root route renders it as its shell, so errors and 404 at any level keep the document and
// the browser bundle.
export function DefaultDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
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
