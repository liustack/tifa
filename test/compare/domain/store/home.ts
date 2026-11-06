// Ported from lang+/route.go: the Layout of the storefront in one language, and its home page.
import { counts } from '../counts.ts'
import { freeShipping } from './cart/index.ts'
import { featured, getProductCard, latestReviews, listCategories, menu, newFilter, search } from './products/index.ts'
import type { Card, MenuGroup, ReviewCard } from './products/index.ts'
import { list } from './journal/post.ts'
import type { Summary } from './journal/post.ts'
import { findLocale } from './store.ts'
import type { Locale } from './store.ts'

export interface Home {
  featured: Card[]
  picks: Card[]
  byCategory: CategoryPicks[]
  sale: Card[]
  reviews: ReviewCard[]
  posts: Summary[]
}

// CategoryPicks are the best rated products of one category.
export interface CategoryPicks {
  id: string
  name: string
  products: Card[]
}

// picks are the products the shop uses every day, which the home page shows on their own.
const picks = ['pour-over-kettle', 'ceramic-cup', 'oak-tray']

// loadHome is the Load of the storefront's home.
export function loadHome(params: { lang: string }): Home {
  counts.page++
  const l = findLocale(params.lang)
  const h: Home = {
    featured: featured(8, l), picks: picks.map((slug) => getProductCard(slug, l)), byCategory: [], sale: [],
    reviews: latestReviews(6, l), posts: list(l).slice(0, 3),
  }
  for (const cat of listCategories(l)) {
    const best = search(newFilter({ category: cat.id, sort: 'rating' }), l).products
    h.byCategory.push({ id: cat.id, name: cat.name, products: best.slice(0, 4) })
  }
  h.sale = search(newFilter({ sort: 'price-desc' }), l).products.filter((p) => p.compareAtCents > 0)
  return h
}

// Shell is what every page of the storefront shows around itself: the language, the category
// menu and the subtotal from which delivery is free.
export interface Shell {
  locale: Locale
  menu: MenuGroup[]
  freeShippingCents: number
}

// layoutLang is the Layout of /store/{lang} and every page below it.
export function layoutLang(params: { lang: string }): Shell {
  counts.shell++
  const l = findLocale(params.lang)
  return { locale: l, menu: menu(l), freeShippingCents: freeShipping(l) }
}
