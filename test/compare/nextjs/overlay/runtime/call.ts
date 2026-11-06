// The module the generated route.gen modules send their requests through, for the Next.js app. It
// does what tifa's call.ts does for a handler function in the browser: sends the call with the
// header that asks for redirects as JSON, reads the answer ({result} or 204), throws TifaError for
// an error, and follows a redirect.
//
// Next.js cannot put a route handler next to the page of the same URL, so a write goes to the same
// path under /compare-write (see test/compare/nextjs/src/app/compare-write). GET /store/<lang>/mine
// has no page, so it stays where it is.

import { TifaError } from './client'

export interface CallOptions {
  signal?: AbortSignal
}

export function segment(value: string) {
  if (value === '' || value === '.' || value === '..') throw new Error(`the path parameter ${JSON.stringify(value)} would send the request to another path`)
  return encodeURIComponent(value)
}

// navigate goes to the target of a redirect through the router of the page. It is set by
// useRouter of the router shim, and the call falls back to loading the page.
let navigate: ((href: string) => void) | undefined
export function setNavigate(fn: (href: string) => void) {
  navigate = fn
}

const callHeader = 'X-Tifa-Call'
const redirectHeader = 'X-Tifa-Redirect'

// writePath is the path a call is sent to.
export function writePath(method: string, path: string) {
  return method === 'GET' || path.endsWith('/mine') ? path : '/compare-write' + path
}

export async function call(method: string, path: string, options: CallOptions & { input?: unknown }, kind: string): Promise<unknown> {
  const headers: Record<string, string> = { Accept: 'application/json', [callHeader]: '1' }
  const init: RequestInit = { method, signal: options.signal, headers, redirect: 'manual' }
  if (options.input !== undefined && method !== 'GET') {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(options.input)
  }
  const response = await fetch(writePath(method, path), init)
  if (response.headers.has(redirectHeader)) {
    const body = (await response.json()) as { redirect: string }
    const target = new URL(body.redirect, window.location.href)
    const href = target.pathname + target.search + target.hash
    if (navigate && target.origin === window.location.origin) navigate(href)
    else window.location.assign(target.href)
    return undefined
  }
  if (!response.ok) {
    let body: { error?: unknown; fields?: unknown } = {}
    try {
      const value: unknown = await response.json()
      if (typeof value === 'object' && value !== null) body = value
    } catch {
      // Something other than the JSON of the server answered.
    }
    const message = typeof body.error === 'string' && body.error ? body.error : response.statusText || String(response.status)
    const fields = body.fields && typeof body.fields === 'object' ? (body.fields as Record<string, string>) : {}
    throw new TifaError(response.status, message, fields)
  }
  if (kind === 'raw') return response
  if (response.status === 204) return undefined
  const body = (await response.json()) as { result?: unknown }
  return kind === 'json' ? body.result : undefined
}
