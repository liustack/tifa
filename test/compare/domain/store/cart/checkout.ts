// Ported from cart/checkout.go: validating an order, placing it, and showing a placed order.
import type { Ctx } from '../ctx.ts'
import { Fields, HttpError, NotFound } from '../errors.ts'
import { inLocale, storefrontProject } from '../store.ts'
import type { Locale } from '../store.ts'
import { DAY, iso } from '../time.ts'
import { createStoreOrder, getOrder } from '../../orders.ts'
import { buy, NoStock } from '../products/edit.ts'
import type { Purchase, Take } from '../products/edit.ts'
import { product } from '../products/product.ts'
import { cartId, carts, cookieName, demoLinesCopy, getDeliveries, newCartId, priceCart, setCartCookie } from './cart.ts'
import type { Delivery } from './cart.ts'
import { countries, deliveries } from './data.ts'
import { Recent } from './recent.ts'

// Address is where an order goes.
export interface Address {
  name: string
  phone: string
  country: string
  region: string
  city: string
  line1: string
  line2: string
  postalCode: string
}

// Order is what the checkout asks for. The browser checks it with the same rules before sending
// it (checkout-schema.ts), and validate checks it again here.
export interface Order {
  email: string
  address: Address
  delivery: string
  payment: string // "card", "alipay" or "wechat"
  cardNumber: string
  cardExpiry: string // MM/YY
  cardCvc: string
  billingSame: boolean
  notes: string
  newsletter: boolean
  terms: boolean
}

export interface Country {
  code: string
  name: string
}

// getCountries returns the countries the shop delivers to, in the language of l. Go calls it Countries.
export function getCountries(l: Locale): Country[] {
  return countries.map((c) => ({ code: c.code, name: inLocale(c.name, l) }))
}

// savedAddresses returns the addresses the visitor used before, for the checkout to offer. The
// second lacks a postal code, which the checkout asks to fill in.
export function savedAddresses(l: Locale): Address[] {
  const a = (x: Partial<Address>): Address => ({ name: '', phone: '', country: '', region: '', city: '', line1: '', line2: '', postalCode: '', ...x })
  if (l.code === 'en-US') {
    return [
      a({ name: 'Emma Walker', phone: '+1 415 555 0134', country: 'US', region: 'CA', city: 'San Francisco', line1: '1 Market Street', line2: 'Suite 300', postalCode: '94105' }),
      a({ name: 'Emma Walker', phone: '+1 415 555 0134', country: 'US', region: 'OR', city: 'Portland', line1: '200 Alder Street' }),
    ]
  }
  return [
    a({ name: '林夏', phone: '138 0013 8000', country: 'CN', region: '上海市', city: '上海市', line1: '徐汇区武康路 99 号', line2: '2 号楼 301', postalCode: '200031' }),
    a({ name: '林夏', phone: '138 0013 8000', country: 'CN', region: '浙江省', city: '杭州市', line1: '西湖区龙井路 1 号' }),
  ]
}

// The most characters (code points) each text field of an order takes, counted after the spaces
// the page trims.
const maxEmail = 254
const maxName = 100
const maxPlace = 100 // region and city
const maxLine = 200 // the two address lines
const maxPostalCode = 20
const maxNotes = 200

const emailPattern = /^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$/
const phonePattern = /^\+?[0-9 ()-]{7,20}$/
const postalPattern: Record<string, RegExp> = {
  CN: /^[0-9]{6}$/,
  US: /^[0-9]{5}(-[0-9]{4})?$/,
  JP: /^[0-9]{3}-?[0-9]{4}$/,
  CA: /^[A-Za-z][0-9][A-Za-z] ?[0-9][A-Za-z][0-9]$/,
}
const expiryPattern = /^(0[1-9]|1[0-2])\/[0-9]{2}$/
const cvcPattern = /^[0-9]{3,4}$/

// runes counts code points, as utf8.RuneCountInString.
const runes = (s: string): number => [...s].length

// validate checks an order field by field and throws Fields with a key the page translates for
// each field that fails. Go calls it Order.Validate.
export function validate(o: Order): void {
  const f: Record<string, string> = {}
  if (runes(o.email) > maxEmail || !emailPattern.test(o.email)) f.email = 'email'
  const a = o.address
  for (const [name, value, max] of [
    ['address.name', a.name, maxName],
    ['address.region', a.region, maxPlace],
    ['address.city', a.city, maxPlace],
    ['address.line1', a.line1, maxLine],
  ] as const) {
    const v = value.trim()
    if (v === '') f[name] = 'required'
    else if (runes(v) > max) f[name] = 'tooLong'
  }
  // The page does not trim the second line, which may be empty.
  if (runes(a.line2) > maxLine) f['address.line2'] = 'tooLong'
  if (!phonePattern.test(a.phone.trim())) f['address.phone'] = 'phone'
  if (!countries.some((c) => c.code === a.country)) f['address.country'] = 'required'
  const postal = a.postalCode.trim()
  const p = Object.hasOwn(postalPattern, a.country) ? postalPattern[a.country] : undefined
  if (runes(postal) > maxPostalCode) f['address.postalCode'] = 'tooLong'
  else if (p && !p.test(postal)) f['address.postalCode'] = 'postalCode'
  else if (!p && postal === '') f['address.postalCode'] = 'required'
  if (!deliveries.some((d) => d.id === o.delivery)) f.delivery = 'required'
  switch (o.payment) {
    case 'card':
      if (!luhn(o.cardNumber)) f.cardNumber = 'card'
      if (!expiryPattern.test(o.cardExpiry)) f.cardExpiry = 'expiry'
      if (!cvcPattern.test(o.cardCvc)) f.cardCvc = 'cvc'
      break
    case 'alipay':
    case 'wechat':
      break
    default:
      f.payment = 'required'
  }
  if (runes(o.notes) > maxNotes) f.notes = 'tooLong'
  if (!o.terms) f.terms = 'terms'
  if (Object.keys(f).length > 0) throw new Fields(f)
}

// luhn reports whether a card number, with spaces, passes the Luhn check and has 13 to 19 digits.
function luhn(number: string): boolean {
  const digits = number.replaceAll(' ', '')
  const bytes = Buffer.byteLength(digits)
  if (bytes < 13 || bytes > 19) return false
  let sum = 0
  for (let i = 0; i < digits.length; i++) {
    const ch = digits[digits.length - 1 - i]
    if (ch < '0' || ch > '9') return false
    let d = ch.charCodeAt(0) - 48
    if (i % 2 === 1) {
      d *= 2
      if (d > 9) d -= 9
    }
    sum += d
  }
  return sum % 10 === 0
}

// Placed is an order placed from a cart, as the confirmation shows it. totalCents is in the
// minor unit of currency, the currency the order was placed in, whatever language shows it.
export interface Placed {
  id: string
  email: string
  count: number
  totalCents: number
  currency: string
  delivery: Delivery
  placedAt: string
}

// Placed orders are kept for their confirmation, at most maxPlaced of them and each for
// placedIdle after it was last shown, like the carts.
const maxPlaced = 10_000
const placedIdle = 30 * DAY

interface PlacedRecord {
  // cart is the ID of the cart the order was placed from. Only a request with that cart sees the order.
  // The rest of the order is the order of the back office.
  cart: string
  totalCents: number
  currency: string
  delivery: string
  at: number
}

// On globalThis for the same reason as the carts.
const g = globalThis as typeof globalThis & { __comparePlaced?: Recent<PlacedRecord> }
const placed = (g.__comparePlaced ??= new Recent<PlacedRecord>(maxPlaced, placedIdle))

// placeOrder places the order of the cart of the request as an order of the studio shop, takes its
// pieces from the stock, empties the cart and returns the number of the order. The order must have
// passed validate. Go calls it PlaceOrder.
export function placeOrder(c: Ctx, l: Locale, o: Order, now: number): string {
  let id = cartId(c)
  let rec = carts.get(id, now)
  if (!rec && id !== '') {
    // The cart was forgotten, or never made here. The checkout showed that cart, so placing the
    // demonstration cart in its place would charge another amount. Drop the cookie, so the page
    // shows the demonstration cart when it loads again.
    c.setCookie({ name: cookieName, value: '', path: '/', maxAge: -1 })
    throw new HttpError(409, 'cartExpired')
  }
  const ok = rec !== undefined
  if (!rec) rec = { lines: demoLinesCopy(), coupon: '' }
  if (rec.lines.length === 0) throw new HttpError(409, 'emptyCart')
  // As in Go, the order and its total come from what was bought, not from the catalogue later.
  const takes: Take[] = rec.lines.map((r) => ({ slug: r.slug, color: r.color, size: r.size, quantity: r.quantity }))
  let bought: Purchase
  try {
    bought = buy(takes)
  } catch (e) {
    if (e instanceof NoStock) throw new HttpError(409, 'cartStock')
    throw e
  }
  const cart = priceCart(rec.lines, rec.coupon, l, (slug, l) => {
    const p = bought.products.get(slug)
    return p ? product(p, l) : null
  })
  const a = o.address
  const orderId = createStoreOrder({ customer: a.name, phone: a.phone, email: o.email, address: address(a, l), note: o.notes, items: [] }, bought.lines, now).id
  if (!ok) {
    // A visitor without a cart, who checks out the demonstration cart, gets a cart of their own,
    // which the order is placed from.
    id = newCartId()
    carts.add(id, rec, now)
  }
  let total = cart.subtotalCents - cart.discountCents
  for (const d of getDeliveries(cart, now, l)) if (d.id === o.delivery) total += d.priceCents
  placed.add(orderId, { cart: id, totalCents: total, currency: l.currency, delivery: o.delivery, at: now }, now)
  rec.lines = []
  rec.coupon = ''
  setCartCookie(c, id)
  return orderId
}

// address is the delivery address of an order as one line, as the back office keeps it, with the
// name of the country in the language of l.
function address(a: Address, l: Locale): string {
  const parts = [a.line1, a.line2, a.city, a.region, a.postalCode].map((p) => p.trim()).filter((p) => p !== '')
  for (const c of countries) if (c.code === a.country) parts.push(inLocale(c.name, l))
  return parts.join(', ')
}

// getPlaced returns an order placed from the cart cartId in the language of l, or throws
// NotFound, also when another cart placed it. It reads the order of the back office. Go calls it
// GetPlaced.
export function getPlaced(id: string, cart: string, l: Locale, now = Date.now()): Placed {
  const r = placed.get(id, now)
  if (!r || cart === '' || r.cart !== cart) throw new NotFound()
  const o = getOrder(storefrontProject, id)
  const p: Placed = {
    id, email: o.email, count: o.items.reduce((n, item) => n + item.quantity, 0), totalCents: r.totalCents, currency: r.currency, placedAt: iso(r.at),
    delivery: { id: '', name: '', priceCents: 0, earliest: '0001-01-01T00:00:00Z', latest: '0001-01-01T00:00:00Z' },
  }
  for (const d of getDeliveries({ subtotalCents: 0, discountCents: 0, freeShippingCents: 1 }, r.at, l)) if (d.id === r.delivery) p.delivery = d
  return p
}
