import {
  createMemoryHistory,
  createRouter,
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
  return createRouter({
    routeTree,
    context: { source },
    isServer: serverURL !== undefined,
    history: serverURL ? createMemoryHistory({ initialEntries: [serverURL] }) : options.history,
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
}

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

// pick keeps the query parameters a Load reads, for loaderDeps and remountDeps. Other
// parameters then neither refetch nor remount.
export function pick(search: object, keys: string[]): Record<string, unknown> {
  const values = search as Record<string, unknown>
  const picked: Record<string, unknown> = {}
  for (const key of keys) picked[key] = values[key]
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
