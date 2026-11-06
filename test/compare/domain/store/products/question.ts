// Ported from products/question.go: the questions asked on a product page.
import { inLocale } from '../store.ts'
import type { Locale } from '../store.ts'
import { DAY, iso } from '../time.ts'
import { productBySlug } from './product.ts'
import { questions } from './questionData.ts'

export interface Question {
  question: string
  answer: string
  byShop: boolean // answered by the shop, not by another customer
  answeredAt: string
  helpful: number
}

// questionsOf returns the questions asked about a product, newest first. Go calls it Questions.
export function questionsOf(slug: string, l: Locale): Question[] {
  const p = productBySlug(slug)!
  const out = (questions[p.category] ?? []).map((qa, i) => ({
    question: inLocale(qa[0], l), answer: inLocale(qa[1], l), byShop: i !== 2,
    answeredAt: iso(p.added + (60 - i * 6) * DAY), helpful: (i * 7 + p.slug.length) % 9,
  }))
  return out
}
