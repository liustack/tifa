// The write routes of the storefront as Request to Response functions, with the answers tifa
// gives: the body and status of a success, and for a failure a JSON body {"error", "fields"}.
//
//   status 200, JSON   a write that returns a value (add to cart, change quantity)
//   status 204         a write that returns nothing (coupon, mark seen)
//   status 303         the checkout, with Location set to the confirmation
//   status 400         the body is not valid JSON, or has a field of the wrong type
//   status 404         NotFound, 422 Fields (with "fields"), 409 and others from HttpError
//
// Only JSON bodies are taken, which is all the storefront's pages send. Every cookie the write
// sets is added to the answer as a Set-Cookie header.
import { Ctx } from './ctx.ts'
import { BadJSON, Num, asInt, parseBody } from './json.ts'
import type { Obj, Value } from './json.ts'
import { Fields, HttpError, NotFound } from './errors.ts'
import { patchCart, postCoupon } from './cart/route.ts'
import { postCheckout } from './checkout.ts'
import { postMine } from './mine.ts'
import { postProduct } from './product.ts'
import type { Order } from './cart/index.ts'

type Kind = 'string' | 'int' | 'bool'
interface Shape {
  [field: string]: Kind | Shape
}

const orderShape: Shape = {
  email: 'string',
  address: { name: 'string', phone: 'string', country: 'string', region: 'string', city: 'string', line1: 'string', line2: 'string', postalCode: 'string' },
  delivery: 'string', payment: 'string', cardNumber: 'string', cardExpiry: 'string', cardCvc: 'string',
  billingSame: 'bool', notes: 'string', newsletter: 'bool', terms: 'bool',
}

class BadBody extends Error {}

// decodeBody reads a JSON body into the shape the Go input struct has, with Go's zero values
// for the fields it does not carry. A value of the wrong type is an error, null is the zero value.
async function decodeBody<T>(request: Request, shape: Shape): Promise<T> {
  const type = (request.headers.get('content-type') ?? '').split(';')[0].trim().toLowerCase()
  if (type !== 'application/json') throw new HttpError(415, 'Unsupported Media Type')
  let parsed: Value
  try {
    parsed = parseBody(new Uint8Array(await request.arrayBuffer()))
  } catch (e) {
    if (e instanceof BadJSON) throw new BadBody()
    throw e
  }
  const fill = (value: Value, s: Shape): Record<string, unknown> => {
    if (value === null) value = new Map() as Obj
    if (!(value instanceof Map)) throw new BadBody()
    const out: Record<string, unknown> = {}
    for (const [name, kind] of Object.entries(s)) {
      const v = value.has(name) ? value.get(name)! : null
      if (typeof kind === 'object') out[name] = fill(v, kind)
      else if (v === null) out[name] = kind === 'string' ? '' : kind === 'bool' ? false : 0
      else if (kind === 'string' && typeof v === 'string') out[name] = v
      else if (kind === 'bool' && typeof v === 'boolean') out[name] = v
      else if (kind === 'int' && v instanceof Num && asInt(v) !== undefined) out[name] = asInt(v)
      else throw new BadBody()
    }
    return out
  }
  return fill(parsed, shape) as T
}

// failure answers an error. Cookies set before the error stay set, as in Go (a forgotten cart's
// cookie is dropped by the 409 that says so).
function failure(c: Ctx, e: unknown): Response {
  const send = (status: number, body: unknown) => answer(c, { status, body })
  if (e instanceof BadBody) return send(400, { error: 'invalid JSON body' })
  if (e instanceof NotFound) return send(404, { error: 'Not Found' })
  // Go writes the keys of a map in order.
  if (e instanceof Fields) {
    const fields = Object.fromEntries(Object.entries(e.fields).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)))
    return send(422, { error: 'Unprocessable Entity', fields })
  }
  if (e instanceof HttpError) return send(e.status, { error: e.message })
  throw e
}

function answer(c: Ctx, init: { status: number; body?: unknown; location?: string }): Response {
  const headers = new Headers()
  if (init.location) headers.set('Location', init.location)
  for (const cookie of c.setCookies) headers.append('Set-Cookie', cookie)
  if (init.body === undefined) return new Response(null, { status: init.status, headers })
  headers.set('Content-Type', 'application/json')
  return new Response(JSON.stringify(init.body), { status: init.status, headers })
}

async function write(request: Request, run: (c: Ctx) => Promise<Response>): Promise<Response> {
  const c = new Ctx(request.headers.get('cookie'))
  try {
    return await run(c)
  } catch (e) {
    return failure(c, e)
  }
}

// postProductRoute is POST /store/{lang}/products/{product}: put the product in the cart.
export function postProductRoute(request: Request, params: { lang: string; product: string }): Promise<Response> {
  return write(request, async (c) => {
    const input = await decodeBody<{ color: string; size: string; quantity: number }>(request, { color: 'string', size: 'string', quantity: 'int' })
    return answer(c, { status: 200, body: postProduct(c, params, input) })
  })
}

// patchCartRoute is PATCH /store/{lang}/cart: change the quantity of a line, answering with the cart.
export function patchCartRoute(request: Request, params: { lang: string }): Promise<Response> {
  return write(request, async (c) => {
    const input = await decodeBody<{ line: string; quantity: number }>(request, { line: 'string', quantity: 'int' })
    return answer(c, { status: 200, body: patchCart(c, params, input) })
  })
}

// postCartRoute is POST /store/{lang}/cart: put a coupon on the cart, or take it off.
export function postCartRoute(request: Request, params: { lang: string }): Promise<Response> {
  return write(request, async (c) => {
    const input = await decodeBody<{ code: string }>(request, { code: 'string' })
    postCoupon(c, params, input)
    return answer(c, { status: 204 })
  })
}

// postCheckoutRoute is POST /store/{lang}/checkout: check the order, place it and redirect to
// its confirmation.
export function postCheckoutRoute(request: Request, params: { lang: string }): Promise<Response> {
  return write(request, async (c) => {
    const order = await decodeBody<Order>(request, orderShape)
    const r = postCheckout(c, params, order)
    return answer(c, { status: r.status, location: r.location })
  })
}

// postMineRoute is POST /store/{lang}/mine: remember a product the visitor looked at.
export function postMineRoute(request: Request, params: { lang: string }): Promise<Response> {
  return write(request, async (c) => {
    const input = await decodeBody<{ slug: string }>(request, { slug: 'string' })
    postMine(c, params, input)
    return answer(c, { status: 204 })
  })
}
