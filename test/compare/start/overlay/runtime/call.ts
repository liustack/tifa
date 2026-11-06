// The module the generated route.gen modules send their requests through, for the Start app: the
// calls of the storefront (add to cart, change a quantity, coupon, checkout, mark a product seen)
// are requests to the server routes of the app. It does what call.ts of Tifa does for these
// calls (.tifa/runtime/call.ts), without the session, the build check and the abort of a call:
// the same headers, the same reading of the answer, the same follow of a redirect through the
// router, the same TifaError for a failed call.
import { TifaError } from './client'

export interface CallOptions {
  signal?: AbortSignal
}

export const callHeader = 'X-Tifa-Call'
export const redirectHeader = 'X-Tifa-Redirect'

interface Router {
  navigate(options: { href: string }): Promise<void>
}

let router: Router | undefined

// setRouter receives the router once the browser has created it.
export function setRouter(r: Router) {
  router = r
}

export function segment(value: string) {
  if (value === '' || value === '.' || value === '..') throw new Error(`the path parameter ${JSON.stringify(value)} would send the request to another path`)
  return encodeURIComponent(value)
}

export async function call(method: string, url: string, options: CallOptions & { input?: unknown }, answer: 'json' | 'none' | 'raw'): Promise<unknown> {
  if (typeof document === 'undefined') throw new Error(`${method} ${url} is called outside the browser`)
  // The server answers a redirect of a call that carries callHeader as JSON, since fetch would
  // follow a 3xx to the HTML of its target.
  const headers: Record<string, string> = { Accept: 'application/json', [callHeader]: '1' }
  const init: RequestInit = { method, signal: options.signal, headers, redirect: 'manual' }
  if (options.input !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(options.input)
  }
  const response = await fetch(url, init)
  if (response.headers.has(redirectHeader)) {
    const body = (await response.clone().json()) as { redirect: string }
    const target = new URL(body.redirect, window.location.href)
    if (router && target.origin === window.location.origin) await router.navigate({ href: target.pathname + target.search + target.hash })
    else window.location.assign(target.href)
    return answer === 'raw' ? response : undefined
  }
  if (answer === 'raw') return response
  if (!response.ok) throw await failure(response)
  if (response.status === 204) return undefined
  const body = (await response.json()) as { result?: unknown }
  return answer === 'json' ? body.result : undefined
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
