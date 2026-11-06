// Ported from cart/route.go: the cart page and its writes.
import { counts } from '../../counts.ts'
import type { Ctx } from '../ctx.ts'
import { related, seen } from '../products/index.ts'
import type { Card } from '../products/index.ts'
import { findLocale } from '../store.ts'
import { cartId, getCart, getDeliveries, offers, setQuantity, applyCoupon } from './cart.ts'
import type { Cart, Delivery, Offer } from './cart.ts'

// View is the cart page: the cart, how it can be delivered, and products to add.
export interface CartView {
  cart: Cart
  deliveries: Delivery[]
  suggested: Card[]
  // offers are the coupons to apply, and seen what the visitor looked at last.
  offers: Offer[]
  seen: Card[]
}

// loadCart is the Load of the cart page. now is in milliseconds.
export function loadCart(c: Ctx, params: { lang: string }, now = Date.now()): CartView {
  counts.page++
  const l = findLocale(params.lang)
  const cart = getCart(cartId(c), l, now)
  const slug = cart.lines.length > 0 ? cart.lines[0].slug : 'ceramic-cup'
  return {
    cart, deliveries: getDeliveries(cart, now, l), suggested: related(slug, 8, l),
    offers: offers(cart.subtotalCents, l), seen: seen(c, '', l),
  }
}

export interface QuantityInput {
  line: string
  quantity: number
}

// patchCart is the PATCH of the cart: it changes the quantity of a line, 0 to remove it, and
// answers with the cart.
export function patchCart(c: Ctx, params: { lang: string }, input: QuantityInput): Cart {
  const l = findLocale(params.lang)
  const id = setQuantity(c, input.line, input.quantity)
  return getCart(id, l)
}

export interface CouponInput {
  code: string
}

// postCoupon is the POST of the cart: it puts a coupon on the cart, or takes it off for an empty code.
export function postCoupon(c: Ctx, _params: { lang: string }, input: CouponInput): void {
  applyCoupon(c, input.code)
}
