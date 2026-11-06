// Ported from products/product+/route.go: the product page, and the POST that puts the product
// into the cart.
import { addToCart, count, getDeliveries } from './cart/index.ts'
import type { Delivery } from './cart/index.ts'
import type { Ctx } from './ctx.ts'
import { bestInCategory, bundle, getProduct, questionsOf, related, reviewPage } from './products/index.ts'
import type { Card, Product, Question, ReviewsPage } from './products/index.ts'
import { atoi } from './products/index.ts'
import { findLocale } from './store.ts'

// Detail is the product page.
export interface Detail {
  product: Product
  reviews: ReviewsPage
  related: Card[]
  // bundle are two products of the same category that go with this one, sold together.
  bundle: Card[]
  questions: Question[]
  // best are the best rated products of the category.
  best: Card[]
  // delivery is when standard delivery would arrive if ordered now.
  delivery: Delivery
}

// loadProduct is the Load of the product page. now is in milliseconds.
export function loadProduct(params: { lang: string; product: string }, query: URLSearchParams, now = Date.now()): Detail {
  const l = findLocale(params.lang)
  const p = getProduct(params.product, l)
  const page = atoi(query.get('reviews') ?? '').n
  return {
    product: p, reviews: reviewPage(p.slug, page, l), bundle: bundle(p.slug, 2, l), related: related(p.slug, 12, l),
    questions: questionsOf(p.slug, l), best: bestInCategory(p.slug, 5, l),
    delivery: getDeliveries({ subtotalCents: 0, discountCents: 0, freeShippingCents: 1 }, now, l)[0],
  }
}

export interface AddInput {
  color: string
  size: string
  quantity: number
}

export interface Added {
  count: number
}

// postProduct is the POST of the product page: it puts the product into the cart and answers
// with the number of pieces in it.
export function postProduct(c: Ctx, params: { lang: string; product: string }, input: AddInput): Added {
  const id = addToCart(c, params.product, input.color, input.size, input.quantity)
  return { count: count(id) }
}
