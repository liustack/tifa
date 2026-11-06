// The write routes of the storefront as a framework serves them to the browser. writes.ts answers
// as tifa does to a plain request (JSON body, 204, a 303 with Location). The generated functions
// of the shop call with X-Tifa-Call and read another form, which tifa writes for that header:
//
//   - a result comes in an object, {"result": ...}, and a 204 stays a 204;
//   - a redirect comes as 200 with {"redirect", "status"} and X-Tifa-Redirect, since fetch
//     would follow a 3xx to the HTML of its target;
//   - the answer says it depends on the header (Vary), an error is never stored (no-store).
//
// A framework app puts these functions behind its routes and keeps to its own part: where the URL
// is, how a route reads its params, how a wrong method is refused.
import { Ctx } from './ctx.ts'
import { NotFound } from './errors.ts'
import { getMine } from './mine.ts'
import { getStore } from './store.ts'
import { patchCartRoute, postCartRoute, postCheckoutRoute, postMineRoute, postProductRoute } from './writes.ts'

export const callHeader = 'X-Tifa-Call'

type Lang = { lang: string }

// forCall turns the plain answer of a write into the answer tifa gives to the request.
export async function forCall(request: Request, plain: Response): Promise<Response> {
  if (plain.status >= 400) {
    const headers = new Headers(plain.headers)
    headers.set('Cache-Control', 'no-store')
    return new Response(plain.body, { status: plain.status, headers })
  }
  const headers = new Headers(plain.headers)
  headers.append('Vary', callHeader)
  if (!request.headers.has(callHeader)) return new Response(plain.body, { status: plain.status, headers })
  if (plain.status >= 300 && plain.status < 400) {
    headers.delete('Location')
    headers.set('Content-Type', 'application/json')
    headers.set('Cache-Control', 'no-store')
    headers.set('X-Tifa-Redirect', String(plain.status))
    return new Response(JSON.stringify({ redirect: plain.headers.get('Location'), status: plain.status }), { status: 200, headers })
  }
  if (plain.status === 200 && headers.get('Content-Type')?.startsWith('application/json')) {
    return new Response(JSON.stringify({ result: await plain.json() }), { status: 200, headers })
  }
  return new Response(plain.body, { status: plain.status, headers })
}

// The five writes. Each takes the request and the params of its URL.
export const callProduct = async (request: Request, params: Lang & { product: string }) => forCall(request, await postProductRoute(request, params))
export const callQuantity = async (request: Request, params: Lang) => forCall(request, await patchCartRoute(request, params))
export const callCoupon = async (request: Request, params: Lang) => forCall(request, await postCartRoute(request, params))
export const callCheckout = async (request: Request, params: Lang) => forCall(request, await postCheckoutRoute(request, params))
export const callSeen = async (request: Request, params: Lang) => forCall(request, await postMineRoute(request, params))

// callMine is GET /store/{lang}/mine: the cart and the products the visitor looked at last, for
// the visitor of the cookies it is sent with.
export function callMine(request: Request, params: Lang): Promise<Response> {
  try {
    return forCall(request, Response.json(getMine(new Ctx(request.headers.get('cookie')), params)))
  } catch (e) {
    if (e instanceof NotFound) return forCall(request, Response.json({ error: 'Not Found' }, { status: 404 }))
    throw e
  }
}

// methodNotAllowed is tifa's answer to a method a route has no handler for. allow is the Allow
// header: GET and HEAD first when the URL has a page or a GET handler, then the other methods.
export function methodNotAllowed(allow: string): Response {
  return Response.json({ error: 'Method Not Allowed' }, { status: 405, headers: { Allow: allow, 'Cache-Control': 'no-store' } })
}

// redirectToPreferred is GET /store: the redirect to the language the browser asks for. It
// depends on Accept-Language and says so. Like every route of tifa it answers a call (X-Tifa-Call)
// with a 200 that carries the redirect. The body is the one of Go's http.Redirect.
export function redirectToPreferred(request: Request): Promise<Response> {
  const r = getStore(request.headers.get('accept-language') ?? '')
  return forCall(request, new Response(`<a href="${r.location}">Found</a>.\n\n`, {
    status: r.status,
    headers: { Location: r.location, Vary: r.vary, 'Content-Type': 'text/html; charset=utf-8' },
  }))
}

// withoutSlash is tifa's answer to a URL with a trailing slash: a 308 to the URL without it. It
// is undefined for any other URL.
export function withoutSlash(url: URL): Response | undefined {
  if (url.pathname.length <= 1 || !url.pathname.endsWith('/')) return undefined
  const location = url.pathname.replace(/\/+$/, '') + url.search
  // The body is the one of Go's http.Redirect.
  return new Response(`<a href="${location}">Permanent Redirect</a>.\n\n`, {
    status: 308,
    headers: { Location: location, 'Content-Type': 'text/html; charset=utf-8' },
  })
}
