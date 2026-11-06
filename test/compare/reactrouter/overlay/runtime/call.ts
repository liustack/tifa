// The module the generated route.gen modules send their requests through: the browser's side of
// tifa's runtime/call.ts for the storefront. A call sends the JSON of the input with the header
// X-Tifa-Call and reads the answer the way tifa's does (see app/compare/store-wire.ts for the
// server). The handlers are on /store/<lang>/_write (mine stays where it is), under /store so the cookies of the storefront reach them, since React Router answers a POST or a PATCH to the
// URL of a page with the page. A redirect goes through React Router, as tifa's goes through its
// router.

import { TifaError } from 'tifa/client'

export interface CallOptions {
  signal?: AbortSignal
}

export function segment(value: string) {
  if (value === '' || value === '.' || value === '..') throw new Error(`the path parameter ${JSON.stringify(value)} would send the request to another path`)
  return encodeURIComponent(value)
}

let navigateTo: ((href: string) => Promise<void> | void) | undefined

// setNavigate receives the navigation of React Router once the page runs in the browser.
export function setNavigate(navigate: (href: string) => Promise<void> | void) {
  navigateTo = navigate
}

type Answer = 'json' | 'none' | 'raw'

export async function call(method: string, path: string, options: CallOptions & { input?: unknown }, answer: Answer): Promise<unknown> {
  if (typeof document === 'undefined') throw new Error(`${method} ${path} is called outside the browser`)
  const headers: Record<string, string> = { Accept: 'application/json', 'X-Tifa-Call': '1' }
  const init: RequestInit = { method, signal: options.signal, headers, redirect: 'manual' }
  if (options.input !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(options.input)
  }
  const response = await fetch(path.replace(/^(\/store\/[^/]+)\/(?!mine\b)/, '$1/_write/'), init)
  if (response.headers.has('X-Tifa-Redirect')) {
    const body = (await response.json()) as { redirect: string }
    const target = new URL(body.redirect, window.location.href)
    if (navigateTo && target.origin === window.location.origin) await navigateTo(target.pathname + target.search + target.hash)
    else window.location.assign(target.href)
    return undefined
  }
  if (answer === 'raw') return response
  if (!response.ok) {
    let body: { error?: string; fields?: Record<string, string> } = {}
    try {
      body = await response.json()
    } catch {
      // The answer is not the JSON of the server.
    }
    throw new TifaError(response.status, body.error || response.statusText || String(response.status), body.fields ?? {})
  }
  if (response.status === 204) return undefined
  const body = (await response.json()) as { result?: unknown }
  return answer === 'json' ? body.result : undefined
}
