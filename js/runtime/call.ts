// The runtime of the handler functions that tifa build writes into each +route module, one per
// method handler of route.go: GET, POST, PUT, PATCH and DELETE.

import { callHeader, isRedirected, redirectHeader } from './data'

// TifaError is what a handler function throws when the server answers with an error. message is
// the public message of the answer, fields the messages per input field of a tifa.Fields.
export class TifaError extends Error {
  readonly status: number
  readonly fields: Record<string, string>
  constructor(status: number, message: string, fields: Record<string, string> = {}) {
    super(message)
    this.name = 'TifaError'
    this.status = status
    this.fields = fields
  }
}

// CallOptions are what every handler function takes besides params and input.
export interface CallOptions {
  // Aborts the request.
  signal?: AbortSignal
  // false keeps the loaded data after a successful POST, PUT, PATCH or DELETE, which otherwise
  // reloads the data of the current page and its Layouts.
  invalidate?: boolean
}

// How the answer of a handler is read: its JSON result, nothing (204), or the Response itself
// for a handler that writes its own response.
export type Answer = 'json' | 'none' | 'raw'

// Router is what a handler function needs of the router of the page: reloading the loaded data
// after a write, and going to the target of a redirect.
// Location is the part of a location of TanStack Router that tells which page it is.
interface Location {
  pathname: string
  search: unknown
  hash: string
}

interface Router {
  invalidate(): Promise<void>
  navigate(options: { href: string }): Promise<void>
  parseLocation(location: { href: string; pathname: string; search: string; hash: string; state: object }): Location
  state: { location: Location }
}

let router: Router | undefined

// setRouter receives the router of the page once the browser has taken it over.
export function setRouter(r: Router) {
  router = r
}

// call sends one request to a method handler. url is the path with the parameters in it. GET
// and DELETE carry the input in the query string, the other methods as a JSON body.
export async function call(
  method: string,
  url: string,
  options: CallOptions & { input?: unknown },
  answer: Answer,
): Promise<unknown> {
  if (typeof document === 'undefined')
    throw new Error(`${method} ${url} is called outside the browser: call a handler function from an event handler or an effect, not while rendering`)
  // The server answers a redirect of a call that carries callHeader as JSON. fetch would follow
  // a 3xx to the HTML of its target, and send the body of a 307 or 308 along.
  const headers: Record<string, string> = { Accept: 'application/json', [callHeader]: '1' }
  const init: RequestInit = { method, signal: options.signal, headers, redirect: 'manual' }
  if (options.input !== undefined) {
    if (method === 'GET' || method === 'DELETE') {
      const query = encodeQuery(options.input as Record<string, unknown>)
      if (query) url += '?' + query
    } else {
      headers['Content-Type'] = 'application/json'
      init.body = JSON.stringify(options.input)
    }
  }
  const response = await fetch(url, init)
  if (response.type === 'opaqueredirect')
    throw new Error(
      `${method} ${url} was answered with a redirect the browser cannot read, from a proxy or from a handler that wrote it itself: return c.Redirect(url, code) from the handler or OnRequest instead`,
    )
  if (response.headers.has(redirectHeader)) {
    const body: unknown = await response.clone().json()
    if (!isRedirected(body)) throw new Error(`${method} ${url} was answered with a redirect without a URL`)
    await follow(body.redirect, method, options)
    return answer === 'raw' ? response : undefined
  }
  if (answer === 'raw') {
    if (response.ok) await refresh(method, options)
    return response
  }
  if (!response.ok) throw await failure(response)
  const result = answer === 'json' ? await response.json() : undefined
  await refresh(method, options)
  return result
}

// follow goes to the target of a redirect: through the router for a URL of this site, so the
// page does not reload, and as a whole page for another site or before the router took over.
// After a write the loaded data goes stale first, so the target and its Layouts load it again,
// in the same request as the navigation. A redirect to the page that is showing reloads its
// data as a call without a redirect does, since the router would show the stale data at once
// and load the new data after.
async function follow(location: string, method: string, options: CallOptions) {
  const target = new URL(location, window.location.href)
  if (target.protocol !== 'http:' && target.protocol !== 'https:')
    throw new Error(`${method} was redirected to ${location}, which is not an http or https URL`)
  if (!router || target.origin !== window.location.origin) {
    window.location.assign(target.href)
    return
  }
  const href = target.pathname + target.search + target.hash
  // The router reads the URL, so a query spelled another way, in another order or encoding,
  // is the same page.
  const parsed = router.parseLocation({ href, pathname: target.pathname, search: target.search, hash: target.hash, state: {} })
  if (samePage(parsed, router.state.location)) return refresh(method, options)
  if (method !== 'GET' && options.invalidate !== false) void router.invalidate()
  await router.navigate({ href })
}

function samePage(a: Location, b: Location) {
  return a.pathname === b.pathname && a.hash === b.hash && sameValue(a.search, b.search)
}

// sameValue compares parsed queries: objects of strings and arrays of strings.
function sameValue(a: unknown, b: unknown): boolean {
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return a === b
  if (Array.isArray(a) !== Array.isArray(b)) return false
  const keys = Object.keys(a)
  const other = b as Record<string, unknown>
  return (
    keys.length === Object.keys(b).length &&
    keys.every((key) => Object.hasOwn(other, key) && sameValue((a as Record<string, unknown>)[key], other[key]))
  )
}

async function refresh(method: string, options: CallOptions) {
  // Before the router takes over the page there is no loaded data to reload.
  if (method !== 'GET' && options.invalidate !== false && router) await router.invalidate()
}

async function failure(response: Response): Promise<TifaError> {
  let body: { error?: unknown; fields?: unknown } = {}
  try {
    const value: unknown = await response.json()
    if (typeof value === 'object' && value !== null) body = value
  } catch {
    // A proxy or the network may answer with something other than the JSON of the server.
  }
  const message = typeof body.error === 'string' && body.error ? body.error : response.statusText || String(response.status)
  const fields = body.fields && typeof body.fields === 'object' ? (body.fields as Record<string, string>) : {}
  return new TifaError(response.status, message, fields)
}

// encodeQuery writes the fields of a GET or DELETE input the way Ctx.BindQuery reads them: one
// parameter per value, a repeated one per element of an array, null and undefined left out.
function encodeQuery(input: Record<string, unknown>) {
  const query = new URLSearchParams()
  for (const [name, value] of Object.entries(input)) {
    for (const item of Array.isArray(value) ? value : [value])
      if (item !== null && item !== undefined) query.append(name, String(item))
  }
  return query.toString()
}

// segment and rest write path parameters into a URL: one segment, or the rest of the path with
// its slashes kept. A value that would make the request path name another route is refused: the
// browser resolves . and .. segments, and the server redirects an empty segment, so the request
// would reach a different handler.
export function segment(value: string) {
  if (value === '' || value === '.' || value === '..') throw movedPath(value)
  return encodeURIComponent(value)
}

export function rest(value: string) {
  const parts = value.split('/')
  if (parts.some((part, i) => part === '.' || part === '..' || (part === '' && i < parts.length - 1))) throw movedPath(value)
  return parts.map(encodeURIComponent).join('/')
}

function movedPath(value: string) {
  return new Error(`the path parameter ${JSON.stringify(value)} would send the request to another path`)
}
