// Ported from checkout/route.go: the checkout page, and the order it places.
import { counts } from '../counts.ts'
import { cartId, getCart, validate, getDeliveries, getPlaced, getCountries, placeOrder, savedAddresses } from './cart/index.ts'
import type { Address, Cart, Country, Delivery, Order, Placed } from './cart/index.ts'
import type { Ctx } from './ctx.ts'
import { tileOf, topRated } from './products/index.ts'
import type { Tile } from './products/index.ts'
import { findLocale, languagePrefix } from './store.ts'

// View is the checkout: the cart and what the form offers, or the order just placed. In the
// second case the other fields are what Go leaves for a View it did not fill: empty lists and
// an empty cart.
export interface CheckoutView {
  cart: Cart
  deliveries: Delivery[]
  countries: Country[]
  saved: Address[]
  email: string
  suggested: Tile[]
  // placed is the order the visitor just placed, from ?placed=, which the page confirms in
  // place of the form.
  placed: Placed | null
}

// loadCheckout is the Load of the checkout page. now is in milliseconds.
export function loadCheckout(c: Ctx, params: { lang: string }, query: URLSearchParams, now = Date.now()): CheckoutView {
  counts.page++
  const l = findLocale(params.lang)
  const id = query.get('placed') ?? ''
  if (id !== '') {
    const p = getPlaced(id, cartId(c), l, now)
    return {
      cart: { lines: [], count: 0, subtotalCents: 0, coupon: '', discountCents: 0, freeShippingCents: 0 },
      deliveries: [], countries: [], saved: [], email: '', suggested: [], placed: p,
    }
  }
  const cart = getCart(cartId(c), l, now)
  return {
    cart, deliveries: getDeliveries(cart, now, l), countries: getCountries(l), saved: savedAddresses(l),
    email: l.code === 'en-US' ? 'emma@example.com' : 'linxia@example.com', suggested: topRated(3, l).map(tileOf), placed: null,
  }
}

export interface CheckoutRedirect {
  status: number
  location: string
}

// postCheckout is the POST of the checkout: it places the order and sends the visitor to its
// confirmation. The order is checked first, as tifa checks an input that has a Validate.
export function postCheckout(c: Ctx, params: { lang: string }, order: Order, now = Date.now()): CheckoutRedirect {
  validate(order)
  const l = findLocale(params.lang)
  const id = placeOrder(c, l, order, now)
  return { status: 303, location: languagePrefix(params.lang) + '/store/checkout?placed=' + id }
}
