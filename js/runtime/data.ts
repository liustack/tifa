// The data protocol between the Go server and the route loaders.
//
// Every Load result travels as a RouteResult. A whole-page render gets them injected by Go
// (Injected), a client navigation asks for them with one request per URL: a GET of the page URL
// with the query parameter _tifa=<build ID>.<digits>, the build the page was rendered with and one
// digit for each Load on the chain of the URL, from the root Layout down, 1 for the ones asked for.
// A router preload adds .p. The answer is an envelope {routes, session}: routes holds a
// RouteResult by route ID, each route read on its own whatever the HTTP status, and session the
// session of the request when the app has sessions (session.tsx). A redirect comes as 200 and Redirected. A server
// built from other sources answers 409, and the browser reloads the current page (skew.ts).

import { acceptSession, beginSession, type SessionEnvelope } from './session'
import { reloadForBuild } from './skew'

// RouteResult is the outcome of one Load. status is 200 with data, 404 when the Load returned
// tifa.ErrNotFound, a redirect status with location when it returned tifa.Redirect, or the status
// of the error with its public message.
export interface RouteResult {
  status: number
  data?: unknown
  error?: string
  location?: string
}

// InjectedResult is a RouteResult as Go puts it into a page: the data of a 200 comes as the JSON
// text Go wrote, in json. The render parses it, and the page carries the same text to the
// browser (see loaderDataAdapter in router.tsx), so both read the same values. A render that
// has a nonce writes a slot in the page where the text goes, and Go fills it with the text after
// the render (internal/ssr/slots.go). The slot is in slot, and the page carries it in place of
// json. JSON.stringify in the VM would write a -0 as 0, and a page that formats it with Intl would
// then hydrate other text than the server rendered. A server render gets the text as an argument
// of startRender instead (see RenderResult in server.tsx), and the boot data of a page that skips
// SSR carries it in json.
export interface InjectedResult {
  status: number
  json?: string
  slot?: string
  error?: string
  location?: string
}

// Injected is the results of the Loads of a page with their JSON texts in json, the form of the
// boot data of a page without SSR (Boot) and of what injected answers from. A server render is
// given RenderInput (server.tsx) instead, whose texts are arguments of startRender of their own.
export interface Injected {
  url: string
  buildId: string
  data: Record<string, InjectedResult>
  // Where the browser keeps its copy of the session, and the session of the request. Only an app
  // with sessions sends a key.
  sessionKey?: string
  session?: SessionEnvelope
}

// Boot is what a page rendered without SSR carries in <script id="tifa-data">: the results of
// its Loads, so the browser renders it without asking for data.
export type Boot = Injected

export const bootElementId = 'tifa-data'

// rootElementId is the id of the element in <body> that React renders the application into.
export const rootElementId = 'tifa-root'

// Slot is where a Load stands in a request: chain lists the IDs of every Load of the URL, from the
// root Layout down, and preload is true for a request of the router's preloading. The chain is
// worked out when it is read, which a server render never does.
export interface Slot {
  chain: () => string[]
  preload: boolean
}

// Source answers the RouteResult of one route at one URL.
export type Source = (url: string, id: string, slot: Slot) => Promise<RouteResult>

// RouteError is what a loader throws for a failed Load. Error components receive it.
export class RouteError extends Error {
  readonly status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'RouteError'
    this.status = status
  }
}

// dataParam is the query parameter of a data request. The server takes it off the URL.
export const dataParam = '_tifa'

// buildHeader carries the build ID of the page on the calls of handler functions and on the
// session request, and a server of another build answers 409 with its own build ID in it, to a data
// request too.
export const buildHeader = 'X-Tifa-Build'

// A handler function of call.ts sends callHeader. A redirect then comes as 200 and Redirected,
// with redirectHeader holding its status, since the result of a handler may look like one.
export const callHeader = 'X-Tifa-Call'
export const redirectHeader = 'X-Tifa-Redirect'

// goJSON holds what the page carries for each object of injected data: the JSON text it was
// parsed from, or the slot Go fills with that text.
export const goJSON = new WeakMap<object, string>()

// injected answers from the data Go injected for a whole-page render. Rendering does no I/O.
export function injected(input: Injected): Source {
  const results = new Map<string, RouteResult>()
  for (const [id, { json, slot, ...result }] of Object.entries(input.data)) {
    if (json !== undefined) {
      const data: unknown = JSON.parse(json)
      if (typeof data === 'object' && data !== null) goJSON.set(data, slot ?? json)
      results.set(id, { ...result, data })
    } else results.set(id, result)
  }
  return async (url, id) => {
    if (!sameURL(url, input.url))
      throw new Error(`render input is for ${input.url}, the router asked for ${url}`)
    const result = results.get(id)
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
  preload: boolean
}

// Batch is the loads asked for one URL in one turn. They share a chain, since one URL has one.
interface Batch {
  url: string
  chain: string[]
  waiters: Map<string, Waiter[]>
}

// dataURL is the URL of the data request for the routes of batch: the page URL, and _tifa with the
// build, a digit for each Load of the chain and .p when every load is a preload.
function dataURL(batch: Batch, build: string) {
  const digits = batch.chain.map((id) => (batch.waiters.has(id) ? '1' : '0')).join('')
  const preload = [...batch.waiters.values()].every((list) => list.every((w) => w.preload))
  const sep = batch.url.includes('?') ? '&' : '?'
  return `${batch.url}${sep}${dataParam}=${build}.${digits}${preload ? '.p' : ''}`
}

// collector merges the loader calls made in one turn of the event loop into one request per URL.
// buildId returns the build the page was rendered with.
export function collector(buildId: () => string): Source {
  let pending = new Map<string, Batch>()
  let scheduled = false
  const flush = () => {
    const batches = pending
    pending = new Map()
    scheduled = false
    for (const batch of batches.values()) void request(batch)
  }
  const request = async ({ url, waiters, ...rest }: Batch) => {
    const seq = beginSession()
    try {
      // A redirect of the server arrives as JSON (Redirected). One that still comes as 3xx,
      // from a proxy say, is not followed with the parameter of this request: the browser loads
      // the URL as a whole page and follows it there.
      const response = await fetch(dataURL({ url, waiters, ...rest }, buildId()), {
        redirect: 'manual',
        headers: { Accept: 'application/json' },
      })
      // The server runs another build and names it in buildHeader (a server of before did not).
      // Reload the current page from it, once however many requests got 409, and leave the
      // loaders pending, so the router commits no state for the old build meanwhile. A late 409
      // of a preload never leaves the current page. A page that has reloaded for that build
      // falls through: the answer is no grouped result, so every loader fails with the 409.
      if (response.status === 409 && reloadForBuild(response.headers.get(buildHeader) ?? '')) return
      if (response.type === 'opaqueredirect') {
        window.location.assign(url)
        return
      }
      const body: unknown = await response.json().catch(() => undefined)
      if (isRedirected(body)) {
        acceptSession(seq, body.session)
        // Every route of the request gets the redirect, and its loader throws redirect().
        for (const list of waiters.values())
          for (const waiter of list) waiter.resolve({ status: body.status, location: body.redirect })
        return
      }
      if (!isEnvelope(body)) throw new RouteError(response.status, errorMessage(response, body))
      acceptSession(seq, body.session)
      // Each route gets its own result, whatever the HTTP status of the whole answer.
      for (const [id, list] of waiters) {
        const result = body.routes[id]
        for (const waiter of list) {
          if (result) waiter.resolve(result)
          else waiter.reject(new Error(`response for ${url} has no data for route ${id}`))
        }
      }
    } catch (error) {
      for (const list of waiters.values()) for (const waiter of list) waiter.reject(error)
    }
  }
  return (url, id, slot) =>
    new Promise<RouteResult>((resolve, reject) => {
      const { preload } = slot
      const chain = slot.chain()
      if (!chain.includes(id)) return reject(new Error(`route ${id} is not on the chain of ${url}: ${chain.join(', ')}`))
      const key = url + '\n' + chain.join(',')
      let batch = pending.get(key)
      if (!batch) pending.set(key, (batch = { url, chain, waiters: new Map() }))
      let list = batch.waiters.get(id)
      if (!list) batch.waiters.set(id, (list = []))
      list.push({ resolve, reject, preload })
      if (!scheduled) {
        scheduled = true
        setTimeout(flush, 0)
      }
    })
}

// Redirected is the answer to a data request that OnRequest, a Load the middleware let
// through, or ServeMux redirected: {"redirect": URL, "status": 3xx}. A handler call that is
// redirected gets the same answer.
export interface Redirected {
  redirect: string
  status: number
  // The session of the request, see session.tsx.
  session?: SessionEnvelope
}

export function isRedirected(body: unknown): body is Redirected {
  return (
    typeof body === 'object' &&
    body !== null &&
    typeof (body as Redirected).redirect === 'string' &&
    typeof (body as Redirected).status === 'number'
  )
}

// DataAnswer is the answer to a data request.
interface DataAnswer {
  routes: Record<string, RouteResult>
  session?: unknown
}

// isEnvelope reports whether body is the answer to a data request: RouteResult by route ID, and
// the session.
function isEnvelope(body: unknown): body is DataAnswer {
  if (typeof body !== 'object' || body === null || Array.isArray(body)) return false
  const routes = (body as { routes?: unknown }).routes
  return (
    typeof routes === 'object' &&
    routes !== null &&
    !Array.isArray(routes) &&
    Object.values(routes).every((item) => typeof item === 'object' && item !== null && typeof item.status === 'number')
  )
}

function errorMessage(response: Response, body: unknown) {
  const error = (body as { error?: unknown } | undefined)?.error
  if (typeof error === 'string' && error !== '') return error
  return response.statusText || `request failed with status ${response.status}`
}
