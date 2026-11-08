// Ported from cart/cart.go: the lines of each visitor's cart, coupons, delivery methods and the
// totals. Carts are kept in memory, and the cookie store_cart holds only the ID of the visitor's
// cart. So a cookie made by one server means a cart only to that server: another one that gets
// an ID it did not make starts a new cart on the first change, as Go does.
import { randomBytes } from 'node:crypto'
import type { Ctx } from '../ctx.ts'
import { Fields, NotFound } from '../errors.ts'
import { colorName, getProductOrNull, hasVariant } from '../products/index.ts'
import { inLocale, price } from '../store.ts'
import type { Locale } from '../store.ts'
import { DAY, iso } from '../time.ts'
import { midnight } from '../../time.ts'
import { deliveries } from './data.ts'
import { Recent } from './recent.ts'

// cookieName is the cookie that holds the ID of the visitor's cart.
export const cookieName = 'store_cart'

// maxQuantity is the most of one line a cart takes.
export const maxQuantity = 10

// Line is one product in one colour and size. Amounts are in the minor unit of the locale's currency.
export interface Line {
  id: string
  slug: string
  name: string
  color: string
  colorName: string
  hex: string
  size: string
  quantity: number
  priceCents: number
  stock: number
}

// Cart is a cart with its totals, before delivery.
export interface Cart {
  lines: Line[]
  count: number // pieces
  subtotalCents: number
  coupon: string
  discountCents: number
  // freeShippingCents is the subtotal from which standard delivery is free.
  freeShippingCents: number
}

// Delivery is a way to get an order. earliest and latest are the instants it may arrive between,
// counted from the time of the request.
export interface Delivery {
  id: string
  name: string
  priceCents: number
  earliest: string
  latest: string
}

interface LineRecord {
  slug: string
  color: string
  size: string
  quantity: number
}

export interface CartRecord {
  lines: LineRecord[]
  coupon: string
}

// A cart nobody has used for cartIdle is forgotten, as its cookie expires then too, and at most
// maxCarts are kept, so visitors cannot fill the memory with carts.
const maxCarts = 10_000
const cartIdle = 30 * DAY

// carts are the carts by ID. Only IDs this module made are keys.
// They live on globalThis, since Next.js loads this module once for its pages and once for the write
// routes, and both must see the same carts.
const g = globalThis as typeof globalThis & { __compareCarts?: Recent<CartRecord> }
export const carts = (g.__compareCarts ??= new Recent<CartRecord>(maxCarts, cartIdle))

// A visitor without a cart sees this one, so the demonstration has something to check out. It
// becomes the visitor's own cart at the first change.
const demoLines = (): LineRecord[] => [
  { slug: 'pour-over-kettle', color: 'mist', size: '', quantity: 1 },
  { slug: 'ceramic-cup', color: 'moss', size: '', quantity: 2 },
  { slug: 'linen-apron', color: 'oat', size: 'M', quantity: 1 },
  { slug: 'reading-candle', color: 'charcoal', size: '', quantity: 1 },
]

// demoLinesCopy is a fresh copy of the demonstration cart's lines.
export const demoLinesCopy = demoLines

// cartId returns the cart ID in the request's cookie, or "" when there is none. Go calls it ID.
export function cartId(c: Ctx): string {
  return c.cookie(cookieName) ?? ''
}

// getCart returns the cart id in the language of l: the demonstration cart for an ID that has none.
// Go calls it Get.
export function getCart(id: string, l: Locale, now = Date.now()): Cart {
  const rec = carts.get(id, now)
  return priceCart(rec ? rec.lines.map((r) => ({ ...r })) : demoLines(), rec ? rec.coupon : '', l)
}

// priceCart returns the cart of lines and a coupon, with its totals in the language of l.
export function priceCart(lines: LineRecord[], coupon: string, l: Locale): Cart {
  const cart: Cart = { lines: [], count: 0, subtotalCents: 0, coupon, discountCents: 0, freeShippingCents: freeShipping(l) }
  for (const r of lines) {
    const p = getProductOrNull(r.slug, l)
    if (!p) continue
    cart.lines.push({
      id: lineId(r.slug, r.color, r.size), slug: r.slug, name: p.name, color: r.color, colorName: colorName(r.color, l),
      hex: p.colors?.find((col) => col.id === r.color)?.hex ?? '', size: r.size, quantity: r.quantity,
      priceCents: p.priceCents, stock: p.stock,
    })
    cart.count += r.quantity
    cart.subtotalCents += p.priceCents * r.quantity
  }
  cart.discountCents = discount(coupon, cart.subtotalCents, l)
  return cart
}

// count returns the number of pieces in the cart id.
export function count(id: string, now = Date.now()): number {
  const rec = carts.get(id, now)
  return (rec ? rec.lines : demoLines()).reduce((n, r) => n + r.quantity, 0)
}

// change runs f on a copy of the cart of the request, and keeps the copy when f succeeds. A
// visitor without a cart, or with an ID this module did not make, gets a cart of their own with a
// new ID, starting from the demonstration cart, but only when f succeeds, so a failed request
// leaves no cart behind. It sets the cookie and returns the ID of the cart.
function change(c: Ctx, f: (rec: CartRecord) => void, now = Date.now()): string {
  let id = cartId(c)
  const rec = carts.get(id, now)
  const next: CartRecord = { lines: (rec ? rec.lines : demoLines()).map((r) => ({ ...r })), coupon: rec ? rec.coupon : '' }
  f(next)
  if (rec) {
    rec.lines = next.lines
    rec.coupon = next.coupon
  } else {
    id = newCartId()
    carts.add(id, next, now)
  }
  setCartCookie(c, id)
  return id
}

const base32 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'

// randomText is Go's rand.Text: 26 characters of the base 32 alphabet.
export function randomText(): string {
  const bytes = randomBytes(26)
  let out = ''
  for (const b of bytes) out += base32[b % 32]
  return out
}

// newCartId returns a random ID no cart has.
export function newCartId(): string {
  for (;;) {
    const id = randomText()
    if (!carts.has(id)) return id
  }
}

export function setCartCookie(c: Ctx, id: string): void {
  // The cookie lives as long as an unused cart, and only the server reads it. It has no Secure
  // flag because the example runs on http://127.0.0.1.
  c.setCookie({ name: cookieName, value: id, path: '/', maxAge: cartIdle / 1000, httpOnly: true, sameSite: 'Lax' })
}

// add puts quantity of a product in a colour and size into the cart of the request, and returns
// the ID of the cart. Go calls it Add.
//
// The catalogue keeps one stock per product, not per colour and size, and each line is held to
// it on its own.
export function addToCart(c: Ctx, slug: string, color: string, size: string, quantity: number): string {
  const { stock, ok } = hasVariant(slug, color, size)
  if (!ok) throw new Fields({ color: 'variant' })
  if (quantity < 1 || quantity > maxQuantity) throw new Fields({ quantity: 'quantity' })
  if (stock === 0) throw new Fields({ quantity: 'soldOut' })
  return change(c, (rec) => {
    for (const r of rec.lines) {
      if (r.slug === slug && r.color === color && r.size === size) {
        r.quantity = Math.min(r.quantity + quantity, maxQuantity, stock)
        return
      }
    }
    rec.lines.push({ slug, color, size, quantity: Math.min(quantity, stock) })
  })
}

// setQuantity changes how many of a line the cart holds, 0 removes the line, and returns the ID
// of the cart. A line takes at most maxQuantity and no more than the stock. Go calls it SetQuantity.
export function setQuantity(c: Ctx, line: string, quantity: number): string {
  if (quantity < 0 || quantity > maxQuantity) throw new Fields({ quantity: 'quantity' })
  return change(c, (rec) => {
    const i = rec.lines.findIndex((r) => lineId(r.slug, r.color, r.size) === line)
    if (i < 0) throw new NotFound()
    const r = rec.lines[i]
    if (quantity > hasVariant(r.slug, r.color, r.size).stock) throw new Fields({ quantity: 'stock' })
    if (quantity === 0) rec.lines.splice(i, 1)
    else rec.lines[i].quantity = quantity
  })
}

// applyCoupon puts a coupon on the cart of the request, or takes it off for "". Go calls it ApplyCoupon.
export function applyCoupon(c: Ctx, code: string): void {
  code = code.trim().toUpperCase()
  if (code !== '' && !(code in coupons)) throw new Fields({ code: 'coupon' })
  change(c, (rec) => {
    rec.coupon = code
  })
}

export function lineId(slug: string, color: string, size: string): string {
  return size === '' ? slug + '.' + color : slug + '.' + color + '.' + size
}

const freeShippingFen = 29900

// freeShipping returns the subtotal from which standard delivery is free, in the locale's currency.
export function freeShipping(l: Locale): number {
  return price(freeShippingFen, l)
}

// coupons are the codes the shop gives out: a share off, or an amount off from a subtotal.
const coupons: Record<string, { percent: number; offFen: number; minimumFen: number }> = {
  SHANHAI10: { percent: 10, offFen: 0, minimumFen: 0 },
  WELCOME30: { percent: 0, offFen: 3000, minimumFen: 20000 },
}

export function discount(code: string, subtotal: number, l: Locale): number {
  if (!Object.hasOwn(coupons, code)) return 0
  const c = coupons[code]
  if (c.percent > 0) return Math.trunc((subtotal * c.percent) / 100)
  if (subtotal >= price(c.minimumFen, l)) return price(c.offFen, l)
  return 0
}

// getDeliveries returns the ways the cart can be delivered from now, with their prices for the
// subtotal: standard delivery is free from freeShippingCents. now is in milliseconds. Go calls it Deliveries.
// A date is a whole day: midnight, in the time zone of the locale, of the day of now plus the days the way
// takes, so the data of a page is the same all day and a render cache can reuse the page (as in Go).
// The pages show the month and the day in that zone.
export function getDeliveries(cart: Pick<Cart, 'subtotalCents' | 'discountCents' | 'freeShippingCents'>, now: number, l: Locale): Delivery[] {
  return deliveries.map((d) => {
    let p = price(d.priceFen, l)
    if (d.id === 'standard' && cart.subtotalCents - cart.discountCents >= cart.freeShippingCents) p = 0
    return { id: d.id, name: inLocale(d.name, l), priceCents: p, earliest: iso(midnight(now, l.timeZone, d.earliestDays)), latest: iso(midnight(now, l.timeZone, d.latestDays)) }
  })
}

// Offer is a coupon the shop gives out, as the cart lists it to apply with one press.
export interface Offer {
  code: string
  // detail says what it takes off and from what subtotal, in the language of the locale.
  detail: string
  // applies is whether the subtotal reaches what the coupon asks for.
  applies: boolean
}

// offers returns the coupons the shop gives out, for a cart with subtotalCents.
export function offers(subtotalCents: number, l: Locale): Offer[] {
  return ['SHANHAI10', 'WELCOME30'].map((code) => {
    const c = coupons[code]
    let detail: string
    if (c.percent > 0 && l.code === 'en-US') detail = `${c.percent}% off everything`
    else if (c.percent > 0) detail = `全场 ${(100 - c.percent) / 10} 折`
    else if (l.code === 'en-US') detail = `$${(price(c.offFen, l) / 100).toFixed(2)} off orders over $${(price(c.minimumFen, l) / 100).toFixed(2)}`
    else detail = `满 ${Math.trunc(c.minimumFen / 100)} 元减 ${Math.trunc(c.offFen / 100)} 元`
    return { code, detail, applies: discount(code, subtotalCents, l) > 0 }
  })
}
