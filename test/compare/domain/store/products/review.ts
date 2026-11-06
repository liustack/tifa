// Ported from products/review.go: the reviews of a product, made up from its slug and rating.
import { inLocale } from '../store.ts'
import type { Locale } from '../store.ts'
import { DAY, HOUR, MINUTE, iso } from '../time.ts'
import { catalogue, reviewCount } from './catalogue.ts'
import type { ProductRecord } from './catalogue.ts'
import { reviewers, reviewTags, reviewTexts } from './reviewData.ts'
import { card, productBySlug } from './product.ts'
import type { Card } from './product.ts'

export interface Review {
  author: string
  rating: number
  title: string
  body: string
  postedAt: string
  verified: boolean // bought in this shop
  helpful: number // how many found it helpful
  // photos are the views of the product the customer took pictures of: "detail" or "scene".
  photos: string[]
  // reply is what the shop answered, empty when it did not.
  reply: string
}

export interface RatingCount {
  stars: number
  count: number
}

export const reviewsPerPage = 10

export interface ReviewTag {
  name: string
  count: number
}

export interface ReviewsPage {
  reviews: Review[]
  ratings: RatingCount[]
  tags: ReviewTag[]
  total: number
  page: number
  pages: number
}

// ratingAbove4 is Go's p.rating*10-40, which is a different number on arm64 than on amd64. The Go
// compiler turns a multiply that feeds an add or subtract into one fused instruction on arm64
// (and on amd64 built for GOAMD64=v3 or higher), which rounds once. The default amd64 build rounds
// the product first. So 4.8*10-40 is 8 on amd64 and 7.999... on arm64, and the bars of the
// ratings differ by a review or two. The port gives each machine what the Go shop gives it, so
// the check against Go and the comparison of the apps hold on both.
function ratingAbove4(rating: number): number {
  const product = rating * 10
  if (process.arch !== 'arm64') return product - 40
  // The exact rounding error of the product (Dekker's split), so that the sum is rounded once.
  const split = 134217729 // 2^27 + 1
  const a = rating * split
  const aHigh = a - (a - rating)
  const aLow = rating - aHigh
  const b = 10 * split
  const bHigh = b - (b - 10)
  const bLow = 10 - bHigh
  const error = aHigh * bHigh - product + aHigh * bLow + aLow * bHigh + aLow * bLow
  return product - 40 + error
}

const recordOf = (slug: string): ProductRecord => productBySlug(slug) as ProductRecord

// reviewPage returns page of the reviews of a product, from 1. A page past the last shows the last.
export function reviewPage(slug: string, page: number, l: Locale): ReviewsPage {
  const p = recordOf(slug)
  const total = reviewCount(p)
  const pages = Math.trunc((total + reviewsPerPage - 1) / reviewsPerPage)
  const out: ReviewsPage = { reviews: [], ratings: [], tags: [], total, pages, page: Math.min(Math.max(page, 1), pages) }
  const list: Review[] = []
  for (let i = (out.page - 1) * reviewsPerPage; i < Math.min(out.page * reviewsPerPage, total); i++) list.push(reviewAt(p, i, l))
  out.reviews = list
  const five = Math.trunc((total * Math.trunc(ratingAbove4(p.rating))) / 10)
  const four = Math.trunc(((total - five) * 2) / 3)
  const three = Math.trunc(((total - five - four) * 2) / 3)
  const two = Math.trunc((total - five - four - three) / 2)
  out.ratings = [
    { stars: 5, count: five }, { stars: 4, count: four }, { stars: 3, count: three }, { stars: 2, count: two },
    { stars: 1, count: total - five - four - three - two },
  ]
  out.tags = reviewTags.map((tag, i) => ({ name: inLocale(tag, l), count: Math.trunc((total * (reviewTags.length - i)) / (reviewTags.length + 2)) }))
  return out
}

// reviews returns the n newest reviews of a product.
export function reviews(slug: string, n: number, l: Locale): Review[] {
  const p = recordOf(slug)
  const list: Review[] = []
  for (let i = 0; i < Math.min(n, reviewCount(p)); i++) list.push(reviewAt(p, i, l))
  return list
}

// reviewAt writes the ith newest review of a product. Reviews follow the days since it was added
// at a steady pace, the newest 75 days after it went on sale.
function reviewAt(p: ProductRecord, i: number, l: Locale): Review {
  const t = reviewTexts[(i + p.slug.length) % reviewTexts.length]
  let stars = 5 - Math.trunc(((i * 7 + p.slug.length) % 3) / 2)
  if (p.rating < 4.5 && i % 3 === 2) stars = 3
  return {
    author: inLocale(reviewers[(i * 5 + p.slug.length) % reviewers.length], l), rating: stars,
    title: inLocale(t.title, l), body: inLocale(t.body, l),
    postedAt: iso(p.added + 75 * DAY - i * (20 * HOUR + 97 * MINUTE)),
    verified: i % 4 !== 3, helpful: (i * 13 + p.slug.length) % 17, photos: reviewPhotos(i), reply: shopReply(i, l),
  }
}

// shopReply is what the shop wrote back to a review: to every fifth, and to a thank you.
function shopReply(i: number, l: Locale): string {
  if (i % 5 !== 1) return ''
  return inLocale(['谢谢你的反馈，我们会转给做这件器物的工坊。有任何问题随时联系客服。', 'Thank you for the feedback. We will pass it on to the workshop that made it. Write to us any time.'], l)
}

// reviewPhotos returns the views the ith review has pictures of: every third review has one or two.
function reviewPhotos(i: number): string[] {
  switch (i % 6) {
    case 0:
      return ['scene', 'detail']
    case 3:
      return ['scene']
  }
  return []
}

// ReviewCard is a review with the product it is about, for the reviews of the home page.
export interface ReviewCard extends Review {
  product: Card
}

// latestReviews returns the newest review with pictures of each of the n newest products.
export function latestReviews(n: number, l: Locale): ReviewCard[] {
  return [...catalogue]
    .sort((a, b) => b.added - a.added)
    .slice(0, n)
    .map((p) => ({ ...reviews(p.slug, 1, l)[0], product: card(p, l) }))
}
