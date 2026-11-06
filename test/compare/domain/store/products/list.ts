// Ported from products/route.go: the product list page.
import { counts } from '../../counts.ts'
import { findLocale } from '../store.ts'
import { newFilter, search, topRated } from './product.ts'
import type { Card, Results } from './product.ts'

export interface List extends Results {
  picks: Card[]
}

// atoi is strconv.Atoi: a number in decimal with an optional sign, and the nearest limit for
// one that does not fit in 64 bits. ok is false for text that is not a number.
export function atoi(s: string): { n: number; ok: boolean } {
  if (!/^[+-]?[0-9]+$/.test(s)) return { n: 0, ok: false }
  const big = BigInt(s)
  const max = (1n << 63n) - 1n
  const min = -(1n << 63n)
  return { n: Number(big > max ? max : big < min ? min : big), ok: big <= max && big >= min }
}

// loadList searches the catalogue with the filters in the query string. A filter that does not
// parse is left out, as if it were not there. Go calls it Load of the products package.
export function loadList(params: { lang: string }, query: URLSearchParams): List {
  counts.page++
  const l = findLocale(params.lang)
  const number = (s: string): number => {
    const { n, ok } = atoi(s)
    return !ok || n < 0 ? 0 : n
  }
  const values = (name: string): string[] => query.getAll(name)
  const results = search(
    newFilter({
      query: query.get('q') ?? '', category: query.get('category') ?? '', colors: values('color'), materials: values('material'),
      badges: values('badge'), rating: number(query.get('rating') ?? ''), min: number(query.get('min') ?? ''), max: number(query.get('max') ?? ''),
      inStock: query.get('stock') === '1', sort: query.get('sort') ?? '', page: number(query.get('page') ?? ''),
    }),
    l,
  )
  return { ...results, picks: topRated(6, l) }
}
