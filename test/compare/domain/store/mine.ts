// Ported from mine/route.go: what the storefront knows about the visitor in front of it, the
// cart and the products they looked at last.
import { cartId, getCart } from './cart/index.ts'
import type { Cart } from './cart/index.ts'
import type { Ctx } from './ctx.ts'
import { markSeen, seen } from './products/index.ts'
import type { Card } from './products/index.ts'
import { findLocale } from './store.ts'

export interface Mine {
  cart: Cart
  seen: Card[]
}

// getMine is the GET of the mine route: it answers for the visitor of the cookies it is sent with.
export function getMine(c: Ctx, params: { lang: string }): Mine {
  const l = findLocale(params.lang)
  return { cart: getCart(cartId(c), l), seen: seen(c, '', l) }
}

export interface SeenInput {
  slug: string
}

// postMine is the POST of the mine route: it remembers that the visitor looked at a product. A
// slug the catalogue does not have is left out.
export function postMine(c: Ctx, _params: { lang: string }, input: SeenInput): void {
  markSeen(c, input.slug)
}
