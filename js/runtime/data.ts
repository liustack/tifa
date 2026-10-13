// The data protocol between the Go server and the route loaders.
//
// Every Load result travels as a RouteResult. A whole-page render gets them injected by Go
// (Injected), a client navigation asks for them with one request per URL: a GET of the page URL
// with the header X-Tifa-Route listing the route IDs and X-Tifa-Build carrying the build ID the
// page was rendered with. The answer is a JSON object of RouteResult by route ID, each route
// read on its own whatever the HTTP status. A server built from other sources answers 409, and
// the browser reloads the current page.

// RouteResult is the outcome of one Load. status is 200 with data, 404 when the Load returned
// tifa.ErrNotFound, a redirect status with location when it returned c.Redirect, or the status
// of the error with its public message.
export interface RouteResult {
  status: number
  data?: unknown
  error?: string
  location?: string
}

// Injected is the input of one whole-page render.
export interface Injected {
  url: string
  buildId: string
  data: Record<string, RouteResult>
}

// Assets are the files of the browser bundle a page loads.
export interface Assets {
  script: string
  style?: string
}

// Boot is what a page rendered without SSR carries in <script id="tifa-data">: the render
// input and the assets, so the browser renders it without asking for data.
export interface Boot extends Injected {
  assets: Assets
}

export const bootElementId = 'tifa-data'

// Source answers the RouteResult of one route at one URL.
export type Source = (url: string, id: string) => Promise<RouteResult>

// RouteError is what a loader throws for a failed Load. Error components receive it.
export class RouteError extends Error {
  readonly status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'RouteError'
    this.status = status
  }
}

export const routeHeader = 'X-Tifa-Route'
export const buildHeader = 'X-Tifa-Build'

// injected answers from the data Go injected for a whole-page render. Rendering does no I/O.
export function injected(input: Injected): Source {
  return async (url, id) => {
    if (!sameURL(url, input.url))
      throw new Error(`render input is for ${input.url}, the router asked for ${url}`)
    const result = input.data[id]
    if (!result) throw new Error(`render input has no data for route ${id}`)
    return result
  }
}

// The router may encode the query differently (%20 or +) from the request URL.
export function sameURL(a: string, b: string) {
  const canonical = (url: string) => {
    const value = new URL(url, 'http://tifa.invalid')
    return value.pathname + '?' + value.searchParams.toString()
  }
  return a === b || canonical(a) === canonical(b)
}

interface Waiter {
  resolve: (result: RouteResult) => void
  reject: (error: unknown) => void
}

// collector merges the loader calls made in one turn of the event loop into one request per URL.
// buildId returns the build the page was rendered with.
export function collector(buildId: () => string): Source {
  let pending = new Map<string, Map<string, Waiter[]>>()
  let scheduled = false
  const flush = () => {
    const batch = pending
    pending = new Map()
    scheduled = false
    for (const [url, waiters] of batch) void request(url, waiters)
  }
  const request = async (url: string, waiters: Map<string, Waiter[]>) => {
    try {
      const response = await fetch(url, {
        headers: {
          Accept: 'application/json',
          [routeHeader]: [...waiters.keys()].join(','),
          [buildHeader]: buildId(),
        },
      })
      if (response.status === 409) {
        // The server runs another build. Reload the current page from it, once however many
        // requests got 409, and leave the loaders pending, so the router commits no state for
        // the old build meanwhile. A late 409 of a preload never leaves the current page.
        reloadOnce()
        return
      }
      const body: unknown = await response.json().catch(() => undefined)
      if (!isGrouped(body)) throw new RouteError(response.status, errorMessage(response, body))
      // Each route gets its own result, whatever the HTTP status of the whole answer.
      for (const [id, list] of waiters) {
        const result = body[id]
        for (const waiter of list) {
          if (result) waiter.resolve(result)
          else waiter.reject(new Error(`response for ${url} has no data for route ${id}`))
        }
      }
    } catch (error) {
      for (const list of waiters.values()) for (const waiter of list) waiter.reject(error)
    }
  }
  return (url, id) =>
    new Promise<RouteResult>((resolve, reject) => {
      let ids = pending.get(url)
      if (!ids) pending.set(url, (ids = new Map()))
      let list = ids.get(id)
      if (!list) ids.set(id, (list = []))
      list.push({ resolve, reject })
      if (!scheduled) {
        scheduled = true
        setTimeout(flush, 0)
      }
    })
}

let reloading = false

function reloadOnce() {
  if (reloading) return
  reloading = true
  window.location.reload()
}

// isGrouped reports whether body is the answer to a data request: RouteResult by route ID.
function isGrouped(body: unknown): body is Record<string, RouteResult> {
  return (
    typeof body === 'object' &&
    body !== null &&
    !Array.isArray(body) &&
    Object.values(body).every((item) => typeof item === 'object' && item !== null && typeof item.status === 'number')
  )
}

function errorMessage(response: Response, body: unknown) {
  const error = (body as { error?: unknown } | undefined)?.error
  if (typeof error === 'string' && error !== '') return error
  return response.statusText || `request failed with status ${response.status}`
}
