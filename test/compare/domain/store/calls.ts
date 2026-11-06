// The write routes of the storefront as a framework serves them to the browser. writes.ts gives
// what a write did (an Outcome: status, value, Location, cookies). Here it is written as the answer
// tifa gives, once, whichever form the request asks for:
//
//   - a plain request gets the value as JSON, a 204, or a 303 with its Location;
//   - a call from the generated functions of the browser (X-Tifa-Call) gets the value in an object,
//     {"result": ...}, a 204 as a 204, and a redirect as a 200 with {"redirect", "status"} and
//     X-Tifa-Redirect, since fetch would follow a 3xx to the HTML of its target;
//   - an answer that is not an error says it depends on the header (Vary), and one that is an error
//     is never stored (no-store).
//
// The value is encoded as JSON once, as tifa encodes the answer of a handler once.
//
// A framework app puts these functions behind its routes and keeps to its own part: where the URL
// is, how a route reads its params, how a wrong method is refused.
import { Ctx } from './ctx.ts'
import { NotFound } from './errors.ts'
import { getMine } from './mine.ts'
import { getStore } from './store.ts'
import { checkoutOutcome, couponOutcome, productOutcome, quantityOutcome, respond, seenOutcome } from './writes.ts'
import type { Outcome } from './writes.ts'

export const callHeader = 'X-Tifa-Call'

type Lang = { lang: string }

const json = (value: unknown) => JSON.stringify(value)

/** answer writes an outcome as the answer to this request. */
export function answer(request: Request, o: Outcome): Response {
  if (o.status >= 400) {
    const res = respond(o)
    res.headers.set('Cache-Control', 'no-store')
    return res
  }
  if (!request.headers.has(callHeader)) {
    const res = respond(o)
    res.headers.append('Vary', callHeader)
    return res
  }
  const headers = new Headers()
  for (const cookie of o.cookies) headers.append('Set-Cookie', cookie)
  headers.append('Vary', callHeader)
  if (o.status >= 300 && o.status < 400) {
    headers.set('Content-Type', 'application/json')
    headers.set('Cache-Control', 'no-store')
    headers.set('X-Tifa-Redirect', String(o.status))
    return new Response(json({ redirect: o.location, status: o.status }), { status: 200, headers })
  }
  if (o.status === 200 && o.body !== undefined) {
    headers.set('Content-Type', 'application/json')
    return new Response(json({ result: o.body }), { status: 200, headers })
  }
  if (o.location) headers.set('Location', o.location)
  return new Response(null, { status: o.status, headers })
}

// The five writes. Each takes the request and the params of its URL.
export const callProduct = async (request: Request, params: Lang & { product: string }) => answer(request, await productOutcome(request, params))
export const callQuantity = async (request: Request, params: Lang) => answer(request, await quantityOutcome(request, params))
export const callCoupon = async (request: Request, params: Lang) => answer(request, await couponOutcome(request, params))
export const callCheckout = async (request: Request, params: Lang) => answer(request, await checkoutOutcome(request, params))
export const callSeen = async (request: Request, params: Lang) => answer(request, await seenOutcome(request, params))

// callMine is GET /store/{lang}/mine: the cart and the products the visitor looked at last, for
// the visitor of the cookies it is sent with.
export async function callMine(request: Request, params: Lang): Promise<Response> {
  const c = new Ctx(request.headers.get('cookie'))
  try {
    return answer(request, { status: 200, body: getMine(c, params), cookies: c.setCookies })
  } catch (e) {
    if (e instanceof NotFound) return answer(request, { status: 404, body: { error: 'Not Found' }, cookies: c.setCookies })
    throw e
  }
}

// methodNotAllowed is tifa's answer to a method a route has no handler for. allow is the Allow
// header: GET and HEAD first when the URL has a page or a GET handler, then the other methods.
export function methodNotAllowed(allow: string): Response {
  return new Response(json({ error: 'Method Not Allowed' }), {
    status: 405,
    headers: { 'Content-Type': 'application/json', Allow: allow, 'Cache-Control': 'no-store' },
  })
}

// redirectToPreferred is GET /store: the redirect to the language the browser asks for. It
// depends on Accept-Language and says so. Like every route of tifa it answers a call (X-Tifa-Call)
// with a 200 that carries the redirect. The body is the one of Go's http.Redirect.
export function redirectToPreferred(request: Request): Response {
  const r = getStore(request.headers.get('accept-language') ?? '')
  const res = answer(request, { status: r.status, location: r.location, text: `<a href="${escapeHTML(r.location)}">Found</a>.\n\n`, cookies: [] })
  res.headers.set('Vary', r.vary + ', ' + callHeader)
  return res
}

// escapeHTML is the escape Go's http.Redirect puts on the URL in the body of its answer.
const escapeHTML = (s: string) => s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&#34;', "'": '&#39;' })[c] as string)

// withoutSlash is tifa's answer to a URL with a trailing slash: a 308 to the URL without it. It
// is undefined for any other URL.
export function withoutSlash(url: URL): Response | undefined {
  if (url.pathname.length <= 1 || !url.pathname.endsWith('/')) return undefined
  const location = url.pathname.replace(/\/+$/, '') + url.search
  // The body is the one of Go's http.Redirect.
  return new Response(`<a href="${escapeHTML(location)}">Permanent Redirect</a>.\n\n`, {
    status: 308,
    headers: { Location: location, 'Content-Type': 'text/html; charset=utf-8' },
  })
}
