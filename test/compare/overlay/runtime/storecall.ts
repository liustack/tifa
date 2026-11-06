// The part of the call module that the storefront's three apps share: what a call to a handler of
// Tifa is on the wire. The generated route.gen modules call it for add to cart, change a quantity,
// coupon, checkout and mark a product seen. It does what tifa's runtime/call.ts does for these
// calls, without the session, the build check and the abort of a call: the JSON of the input with
// the header X-Tifa-Call (which asks the server to answer a redirect as JSON, since fetch would
// follow a 3xx to the HTML of its target), the same reading of the answer ({result} or 204), a
// TifaError for a failed call, and the redirect followed through the router of the app.
//
// What differs by app is given to makeCall: the URL a write is sent to (Next.js and React Router
// cannot answer a POST or PATCH at the URL of a page) and how to go to a page.
import { TifaError } from './client'

export interface CallOptions {
  signal?: AbortSignal
}

export function segment(value: string) {
  if (value === '' || value === '.' || value === '..') throw new Error(`the path parameter ${JSON.stringify(value)} would send the request to another path`)
  return encodeURIComponent(value)
}

type Answer = 'json' | 'none' | 'raw'

export interface Setup {
  /** writeURL is the URL a call is sent to. */
  writeURL(method: string, path: string): string
  /** navigate returns the navigation of the app's router once the page runs, or nothing before. */
  navigate(): ((href: string) => Promise<void> | void) | undefined
}

export function makeCall({ writeURL, navigate }: Setup) {
  return async function call(method: string, path: string, options: CallOptions & { input?: unknown }, answer: Answer): Promise<unknown> {
    if (typeof document === 'undefined') throw new Error(`${method} ${path} is called outside the browser`)
    const headers: Record<string, string> = { Accept: 'application/json', 'X-Tifa-Call': '1' }
    const init: RequestInit = { method, signal: options.signal, headers, redirect: 'manual' }
    if (options.input !== undefined && method !== 'GET') {
      headers['Content-Type'] = 'application/json'
      init.body = JSON.stringify(options.input)
    }
    const response = await fetch(writeURL(method, path), init)
    if (response.headers.has('X-Tifa-Redirect')) {
      const body = (await response.clone().json()) as { redirect: string }
      const target = new URL(body.redirect, window.location.href)
      const go = navigate()
      if (go && target.origin === window.location.origin) await go(target.pathname + target.search + target.hash)
      else window.location.assign(target.href)
      return answer === 'raw' ? response : undefined
    }
    if (answer === 'raw') return response
    if (!response.ok) {
      let body: { error?: unknown; fields?: unknown } = {}
      try {
        const value: unknown = await response.json()
        if (typeof value === 'object' && value !== null) body = value
      } catch {
        // A proxy or the network may answer with something other than the JSON of the server.
      }
      const message = typeof body.error === 'string' && body.error ? body.error : response.statusText || String(response.status)
      const fields = body.fields && typeof body.fields === 'object' ? (body.fields as Record<string, string>) : {}
      throw new TifaError(response.status, message, fields)
    }
    if (response.status === 204) return undefined
    const body = (await response.json()) as { result?: unknown }
    return answer === 'json' ? body.result : undefined
  }
}
