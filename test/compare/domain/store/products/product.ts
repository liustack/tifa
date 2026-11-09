// Ported from products/product.go: the products, their search and the cards that lists show.
import { counts } from '../../counts.ts'
import { NotFound } from '../errors.ts'
import { inLocale, price } from '../store.ts'
import type { Locale } from '../store.ts'
import { iso } from '../time.ts'
import { badges, careNotes, catalogue, categories, colorName, colors, materialFamilies, materialGroups, reviewCount } from './catalogue.ts'
import type { ProductRecord } from './catalogue.ts'
import { specGroups } from './spec.ts'
import type { SpecGroup } from './spec.ts'

export interface Color {
  id: string
  name: string
  hex: string
}

export interface Category {
  id: string
  name: string
}

export interface Spec {
  label: string
  value: string
}

// Card is a product as a list shows it. Amounts are in the minor unit of the locale's currency.
// A list Go leaves nil is an empty list in its JSON (encoding/json/v2 writes a nil slice as []).
export interface Card {
  slug: string
  photo: string // the folder of its photos, which another product can share
  name: string
  category: string
  priceCents: number
  compareAtCents: number // 0 when not on sale
  rating: number
  reviews: number
  colors: Color[]
  badge: string // "new", "sale", "limited" or ""
  inStock: boolean
  stock: number
  summary: string
  materials: string[]
  // number is the place of the product in the catalogue, from 1.
  number: number
  sized: boolean
}

// Product is the whole product, as its page shows it.
export interface Product extends Card {
  categoryId: string
  description: string
  sizes: string[]
  care: string[]
  specs: Spec[]
  specGroups: SpecGroup[]
  addedAt: string
}

export function productBySlug(slug: string): ProductRecord | undefined {
  return catalogue.find((p) => p.slug === slug)
}

// getProduct returns the product slug in the language of l, or throws NotFound.
export function getProduct(slug: string, l: Locale): Product {
  const p = productBySlug(slug)
  if (!p) throw new NotFound()
  return product(p, l)
}

// compareRating is Go's compareRating: -1, 0 or 1 by rating.
export function compareRating(a: ProductRecord, b: ProductRecord): number {
  return a.rating < b.rating ? -1 : a.rating > b.rating ? 1 : 0
}

// related returns up to n other products of the same category, then of others, best rated first.
export function related(slug: string, n: number, l: Locale): Card[] {
  const self = productBySlug(slug)
  const others = catalogue.filter((p) => p.slug !== slug)
  others.sort((a, b) => {
    const as = a.category === self?.category
    const bs = b.category === self?.category
    if (as !== bs) return as ? -1 : 1
    return compareRating(b, a)
  })
  return others.slice(0, n).map((p) => card(p, l))
}

// featured returns the n products the home page shows: the newest first.
export function featured(n: number, l: Locale): Card[] {
  return [...catalogue].sort((a, b) => b.added - a.added).slice(0, n).map((p) => card(p, l))
}

export function listCategories(l: Locale): Category[] {
  return categories.map((c) => ({ id: c.id, name: inLocale(c.name, l) }))
}

// Filter is what the product list is searched and filtered by. min and max are whole units of
// the locale's currency, 0 for no bound.
export interface Filter {
  query: string
  category: string
  colors: string[]
  min: number
  max: number
  inStock: boolean
  materials: string[]
  badges: string[]
  // rating is the lowest rating, in tenths of a star: 46 for 4.6 and up. 0 for any.
  rating: number
  sort: string
  page: number
}

// newFilter is a Filter with Go's zero values for what is left out.
export function newFilter(f: Partial<Filter> = {}): Filter {
  return { query: '', category: '', colors: [], min: 0, max: 0, inStock: false, materials: [], badges: [], rating: 0, sort: '', page: 0, ...f }
}

export const sorts = ['featured', 'newest', 'price-asc', 'price-desc', 'rating']

export const pageSize = 24

export interface Facet {
  id: string
  name: string
  hex?: string
  count: number
}

export interface PriceBand {
  min: number
  max: number
  count: number
}

const bandEdges: Record<string, number[]> = { CNY: [100, 200, 400], USD: [15, 30, 60] }

export interface Results {
  filter: Filter
  products: Card[]
  total: number
  pages: number
  categories: Facet[]
  colors: Facet[]
  materials: Facet[]
  badges: Facet[]
  ratings: Facet[]
  bands: PriceBand[]
  priceMax: number
}

const rated = (p: ProductRecord) => Math.round(p.rating * 10)

// search returns the page of the list the filter asks for, in the language of l.
export function search(filter: Filter, l: Locale): Results {
  const f = { ...filter }
  if (!sorts.includes(f.sort)) f.sort = 'featured'
  const r: Results = {
    filter: f, products: [], total: 0, pages: 0, categories: [], colors: [], materials: [], badges: [], ratings: [], bands: [], priceMax: 0,
  }
  // match tells whether a product passes every filter but the one named by skip, which a facet
  // leaves out so that it counts what choosing each of its values would leave.
  const match = (p: ProductRecord, skip: string): boolean => {
    const q = f.query.trim().toLowerCase()
    if (q !== '' && !p.name[0].toLowerCase().includes(q) && !p.name[1].toLowerCase().includes(q) && !p.tags.some((t) => t.includes(q))) return false
    if (skip !== 'category' && f.category !== '' && p.category !== f.category) return false
    if (skip !== 'colors' && f.colors && f.colors.length > 0 && !p.colors.some((c) => f.colors!.includes(c))) return false
    if (skip !== 'materials' && f.materials && f.materials.length > 0 && !materialFamilies(p).some((m) => f.materials!.includes(m))) return false
    if (skip !== 'badges' && f.badges && f.badges.length > 0 && !f.badges.includes(p.badge)) return false
    if (skip !== 'rating' && f.rating > 0 && rated(p) < f.rating) return false
    if (skip !== 'price') {
      const amount = Math.trunc(price(p.priceFen, l) / 100)
      if ((f.min > 0 && amount < f.min) || (f.max > 0 && amount > f.max)) return false
    }
    return !f.inStock || p.stock > 0
  }
  const found: ProductRecord[] = []
  for (const p of catalogue) {
    r.priceMax = Math.max(r.priceMax, Math.trunc((price(p.priceFen, l) + 99) / 100))
    if (match(p, '')) found.push(p)
  }
  // count is how many products pass the other filters and also satisfy in.
  const count = (skip: string, inSet: (p: ProductRecord) => boolean): number => catalogue.filter((p) => inSet(p) && match(p, skip)).length
  for (const c of categories) r.categories.push({ id: c.id, name: inLocale(c.name, l), count: count('category', (p) => p.category === c.id) })
  for (const c of colors) r.colors.push({ id: c.id, name: inLocale(c.name, l), hex: c.hex, count: count('colors', (p) => p.colors.includes(c.id)) })
  for (const m of materialGroups) r.materials.push({ id: m.id, name: inLocale(m.name, l), count: count('materials', (p) => materialFamilies(p).includes(m.id)) })
  for (const b of badges) r.badges.push({ id: b.id, name: inLocale(b.name, l), count: count('badges', (p) => p.badge === b.id) })
  for (const floor of [48, 46, 44]) r.ratings.push({ id: String(floor), name: '', count: count('rating', (p) => rated(p) >= floor) })
  const edges = bandEdges[l.currency]
  for (let i = 0; i <= edges.length; i++) {
    const b: PriceBand = { min: i > 0 ? edges[i - 1] : 0, max: i < edges.length ? edges[i] - 1 : 0, count: 0 }
    b.count = count('price', (p) => {
      const amount = Math.trunc(price(p.priceFen, l) / 100)
      return amount >= b.min && (b.max === 0 || amount <= b.max)
    })
    r.bands.push(b)
  }
  switch (f.sort) {
    case 'newest':
      found.sort((a, b) => b.added - a.added)
      break
    case 'price-asc':
      found.sort((a, b) => a.priceFen - b.priceFen)
      break
    case 'price-desc':
      found.sort((a, b) => b.priceFen - a.priceFen)
      break
    case 'rating':
      found.sort((a, b) => compareRating(b, a))
      break
  }
  r.total = found.length
  r.pages = Math.max(1, Math.trunc((found.length + pageSize - 1) / pageSize))
  f.page = Math.min(Math.max(f.page, 1), r.pages)
  const from = (f.page - 1) * pageSize
  r.products = found.slice(from, Math.min(from + pageSize, found.length)).map((p) => card(p, l))
  return r
}

// hasVariant reports whether a product comes in the colour and size, and how many are in stock.
export function hasVariant(slug: string, color: string, size: string): { stock: number; ok: boolean } {
  const p = productBySlug(slug)
  if (!p) return { stock: 0, ok: false }
  const sizeOK = (p.sizes.length === 0 && size === '') || p.sizes.includes(size)
  return { stock: p.stock, ok: p.colors.includes(color) && sizeOK }
}

export { colorName }

export function card(p: ProductRecord, l: Locale): Card {
  const cat = categories.find((c) => c.id === p.category)
  return {
    slug: p.slug, photo: p.photo, name: inLocale(p.name, l), category: cat ? inLocale(cat.name, l) : '',
    priceCents: price(p.priceFen, l), compareAtCents: p.compareFen > 0 ? price(p.compareFen, l) : 0,
    rating: p.rating, reviews: reviewCount(p),
    colors: p.colors.flatMap((id) => colors.filter((c) => c.id === id).map((c) => ({ id, name: inLocale(c.name, l), hex: c.hex }))),
    badge: p.badge, inStock: p.stock > 0, stock: p.stock, summary: inLocale(p.summary, l),
    materials: p.materials.map((m) => inLocale(m, l)),
    number: catalogue.findIndex((r) => r.slug === p.slug) + 1, sized: p.sizes.length > 0,
  }
}

export function product(p: ProductRecord, l: Locale): Product {
  counts.products++
  const c = card(p, l)
  return {
    ...c, categoryId: p.category, description: inLocale(p.description, l),
    sizes: p.sizes, care: (careNotes[p.category] ?? []).map((n) => inLocale(n, l)),
    specs: p.specs.map((s) => ({ label: inLocale(s[0], l), value: inLocale(s[1], l) })),
    specGroups: specGroups(p, l, c.number), addedAt: iso(p.added),
  }
}

// topRated returns the n best rated products, the cheaper first among equals.
export function topRated(n: number, l: Locale): Card[] {
  return [...catalogue].sort((a, b) => compareRating(b, a) || a.priceFen - b.priceFen).slice(0, n).map((p) => card(p, l))
}

// bestInCategory returns the n best rated products of the category of slug, leaving slug out.
export function bestInCategory(slug: string, n: number, l: Locale): Card[] {
  const self = productBySlug(slug)
  return catalogue
    .filter((p) => p.category === self?.category && p.slug !== slug)
    .sort((a, b) => compareRating(b, a))
    .slice(0, n)
    .map((p) => card(p, l))
}

// getProductOrNull is getProduct that returns null for a slug the catalogue does not have, for
// the code that skips such a slug, as Go's `if err != nil { continue }`.
export function getProductOrNull(slug: string, l: Locale): Product | null {
  const p = productBySlug(slug)
  return p ? product(p, l) : null
}

// getProductCard is the Card of the product slug, which Go reaches as GetProduct(slug).Card. The
// product is built whole, since that is the work Go does for it.
export function getProductCard(slug: string, l: Locale): Card {
  // The whole product is built first, as Go does, then only its Card is kept.
  const { categoryId, description, sizes, care, specs, specGroups, addedAt, ...kept } = getProduct(slug, l)
  return kept
}
