// The write routes of the storefront as Request to Response functions, with the answers tifa
// gives: the body and status of a success, and for a failure a JSON body {"error", "fields"}.
//
//   status 200, JSON   a write that returns a value (add to cart, change quantity)
//   status 204         a write that returns nothing (coupon, mark seen)
//   status 303         the checkout, with Location set to the confirmation
//   status 400         the body is not valid JSON, or has a field of the wrong type
//   status 404         NotFound, 422 Fields (with "fields"), 409 and others from HttpError
//
// A write gives an Outcome: the status, the value to encode, the Location and the cookies. The
// answer is made from it once, as a plain one (respond) or, for a call from the browser, in the
// envelope calls.ts writes, so that the value is encoded once either way.
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
    parsed = parseBody(new Uint8Array(await request.arrayBuffer()), shape)
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
      else if (kind === 'int' && v instanceof Num) {
        const n = asInt(v)
        if (n === undefined) throw new BadBody()
        out[name] = n
      } else throw new BadBody()
    }
    return out
  }
  return fill(parsed, shape) as T
}

// Outcome is what a write did, before it is written as an answer.
export interface Outcome {
  status: number
  /** body is the value that is encoded as JSON, or nothing. */
  body?: unknown
  /** text is a body that is not JSON (the link of a redirect). */
  text?: string
  location?: string
  cookies: string[]
}

// failure is the outcome of an error. Cookies set before the error stay set, as in Go (a forgotten
// cart's cookie is dropped by the 409 that says so).
function failure(c: Ctx, e: unknown): Outcome {
  const send = (status: number, body: unknown): Outcome => ({ status, body, cookies: c.setCookies })
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

/** respond writes an outcome as tifa answers a plain request: the value as JSON, a 3xx with its Location. */
export function respond(o: Outcome): Response {
  const headers = new Headers()
  if (o.location) headers.set('Location', o.location)
  for (const cookie of o.cookies) headers.append('Set-Cookie', cookie)
  if (o.text !== undefined) {
    headers.set('Content-Type', 'text/html; charset=utf-8')
    return new Response(o.text, { status: o.status, headers })
  }
  if (o.body === undefined) return new Response(null, { status: o.status, headers })
  headers.set('Content-Type', 'application/json')
  return new Response(JSON.stringify(o.body), { status: o.status, headers })
}

async function write(request: Request, run: (c: Ctx) => Promise<Omit<Outcome, 'cookies'>>): Promise<Outcome> {
  const c = new Ctx(request.headers.get('cookie'))
  try {
    return { ...(await run(c)), cookies: c.setCookies }
  } catch (e) {
    return failure(c, e)
  }
}

// productOutcome is POST /store/products/{product}: put the product in the cart.
export function productOutcome(request: Request, params: { lang: string; product: string }): Promise<Outcome> {
  return write(request, async (c) => {
    const input = await decodeBody<{ color: string; size: string; quantity: number }>(request, { color: 'string', size: 'string', quantity: 'int' })
    return { status: 200, body: postProduct(c, params, input) }
  })
}

// quantityOutcome is PATCH /store/cart: change the quantity of a line, answering with the cart.
export function quantityOutcome(request: Request, params: { lang: string }): Promise<Outcome> {
  return write(request, async (c) => {
    const input = await decodeBody<{ line: string; quantity: number }>(request, { line: 'string', quantity: 'int' })
    return { status: 200, body: patchCart(c, params, input) }
  })
}

// couponOutcome is POST /store/cart: put a coupon on the cart, or take it off.
export function couponOutcome(request: Request, params: { lang: string }): Promise<Outcome> {
  return write(request, async (c) => {
    const input = await decodeBody<{ code: string }>(request, { code: 'string' })
    postCoupon(c, params, input)
    return { status: 204 }
  })
}

// checkoutOutcome is POST /store/checkout: check the order, place it and redirect to its
// confirmation.
export function checkoutOutcome(request: Request, params: { lang: string }): Promise<Outcome> {
  return write(request, async (c) => {
    const order = await decodeBody<Order>(request, orderShape)
    const r = postCheckout(c, params, order)
    return { status: r.status, location: r.location }
  })
}

// seenOutcome is POST /store/mine: remember a product the visitor looked at.
export function seenOutcome(request: Request, params: { lang: string }): Promise<Outcome> {
  return write(request, async (c) => {
    const input = await decodeBody<{ slug: string }>(request, { slug: 'string' })
    postMine(c, params, input)
    return { status: 204 }
  })
}

// The same as plain Request to Response functions.
export const postProductRoute = async (request: Request, params: { lang: string; product: string }) => respond(await productOutcome(request, params))
export const patchCartRoute = async (request: Request, params: { lang: string }) => respond(await quantityOutcome(request, params))
export const postCartRoute = async (request: Request, params: { lang: string }) => respond(await couponOutcome(request, params))
export const postCheckoutRoute = async (request: Request, params: { lang: string }) => respond(await checkoutOutcome(request, params))
export const postMineRoute = async (request: Request, params: { lang: string }) => respond(await seenOutcome(request, params))
