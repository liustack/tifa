// The writes of the storefront, as server routes on the URLs of the Tifa handlers: add to cart,
// change a quantity, coupon, checkout, mark a product seen, and the redirect of /store. The port
// (test/compare/domain/store/writes.ts) runs the function ported from each Go handler and answers
// as Tifa answers a request with a plain body. This module only adds what Tifa does for a call
// from the browser, which sends X-Tifa-Call:
//
//   - a result comes in an object, {"result": ...};
//   - a redirect comes as 200 with {"redirect", "status"} and X-Tifa-Redirect, since fetch would
//     follow a 3xx to the HTML of its target.

import * as store from '../../../domain/store/index.ts'

async function forCall(request: Request, answer: Promise<Response>): Promise<Response> {
  const res = await answer
  if (!request.headers.has('X-Tifa-Call')) return res
  const headers = new Headers(res.headers)
  if (res.status >= 300 && res.status < 400) {
    headers.delete('Location')
    headers.set('Content-Type', 'application/json')
    headers.set('X-Tifa-Redirect', String(res.status))
    return new Response(JSON.stringify({ redirect: res.headers.get('Location'), status: res.status }), { status: 200, headers })
  }
  if (res.status === 200 && headers.get('Content-Type')?.startsWith('application/json'))
    return new Response(JSON.stringify({ result: await res.json() }), { status: 200, headers })
  return res
}

export const addToCart = (request: Request, product: string, lang: string) =>
  forCall(request, store.postProductRoute(request, { lang, product }))

export const changeQuantity = (request: Request, lang: string) => forCall(request, store.patchCartRoute(request, { lang }))

export const applyCoupon = (request: Request, lang: string) => forCall(request, store.postCartRoute(request, { lang }))

export const placeOrder = (request: Request, lang: string) => forCall(request, store.postCheckoutRoute(request, { lang }))

export const markSeen = (request: Request, lang: string) => forCall(request, store.postMineRoute(request, { lang }))

// GET mine answers for the visitor of the cookies it is sent with.
export function mine(request: Request, lang: string): Response {
  try {
    return Response.json(store.getMine(new store.Ctx(request.headers.get('cookie')), { lang }))
  } catch (e) {
    if (e instanceof store.NotFound) return Response.json({ error: 'Not Found' }, { status: 404 })
    throw e
  }
}

// GET /store: the redirect to the language the browser asks for, which depends on
// Accept-Language and says so.
export function redirectToPreferred(request: Request): Response {
  const { status, location, vary } = store.getStore(request.headers.get('accept-language') ?? '')
  return new Response(null, { status, headers: { Location: location, Vary: vary } })
}
