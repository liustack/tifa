import {
  createMemoryHistory,
  createRouter,
  HeadContent,
  notFound,
  Outlet,
  Scripts,
  type AnyRoute,
  type ErrorComponentProps,
} from '@tanstack/react-router'
import { RouteError, type Source } from './data'

// RouterContext is the context of the root route. Loaders read their data through source.
export interface RouterContext {
  source: Source
}

interface Dehydrated {
  buildId: string
}

export interface RouterOptions {
  // The build ID a server render writes into the page.
  buildId?: string
  // The URL of a server render. Without it the router runs in the browser.
  serverURL?: string
  // Receives the build ID of the page when the browser hydrates it.
  onHydrate?: (buildId: string) => void
}

export function createAppRouter<TRouteTree extends AnyRoute>(
  routeTree: TRouteTree,
  source: Source,
  options: RouterOptions,
) {
  const { buildId, serverURL, onHydrate } = options
  return createRouter({
    routeTree,
    context: { source },
    isServer: serverURL !== undefined,
    history: serverURL ? createMemoryHistory({ initialEntries: [serverURL] }) : undefined,
    defaultPreload: 'intent',
    scrollRestoration: true,
    dehydrate: (): Dehydrated => {
      if (buildId === undefined) throw new Error('a server render needs the build ID')
      return { buildId }
    },
    hydrate: (dehydrated: Dehydrated) => {
      if (onHydrate) onHydrate(dehydrated.buildId)
    },
  })
}

// load is the loader of every route with a Go Load: it asks the source for the route's result
// at the URL being loaded and turns 404 into notFound() and other failures into a RouteError.
export async function load<T>(
  context: RouterContext,
  id: string,
  location: { pathname: string; searchStr: string },
): Promise<T> {
  const result = await context.source(location.pathname + location.searchStr, id)
  if (result.status === 200) return result.data as T
  if (result.status === 404) throw notFound()
  throw new RouteError(result.status, result.error ?? `route ${id} failed with status ${result.status}`)
}

// pick keeps the query parameters a Load reads, for loaderDeps and remountDeps. Other
// parameters then neither refetch nor remount.
export function pick(search: object, keys: string[]): Record<string, unknown> {
  const values = search as Record<string, unknown>
  const picked: Record<string, unknown> = {}
  for (const key of keys) picked[key] = values[key]
  return picked
}

// DefaultDocument is the root component when the application has no app/layout.tsx.
export function DefaultDocument() {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <HeadContent />
      </head>
      <body>
        <Outlet />
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
