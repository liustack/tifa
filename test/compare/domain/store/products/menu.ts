// Ported from products/menu.go: the category menu and the products bought together.
import { inLocale } from '../store.ts'
import type { Locale } from '../store.ts'
import { catalogue, categories } from './catalogue.ts'
import { card, compareRating, productBySlug } from './product.ts'
import type { Card, Category } from './product.ts'
import { kinds } from './kinds.ts'

// MenuGroup is one category of the store's category menu: its kinds of products, how many
// products it has, and the products the menu shows beside them.
export interface MenuGroup extends Category {
  count: number
  kinds: string[]
  featured: Card[]
}

// menu returns the category menu: each category with its kinds and its two best rated products.
export function menu(l: Locale): MenuGroup[] {
  return categories.map((c) => {
    const inCategory = catalogue.filter((p) => p.category === c.id)
    const k = (kinds[c.id] ?? []).map((x) => inLocale(x, l))
    const best = [...inCategory].sort((a, b) => compareRating(b, a)).slice(0, 2).map((p) => card(p, l))
    return { id: c.id, name: inLocale(c.name, l), count: inCategory.length, kinds: k, featured: best }
  })
}

// bundle returns up to n products to buy together with slug: others of its category that come in
// one size, best rated first, so each goes into the cart in its first colour.
export function bundle(slug: string, n: number, l: Locale): Card[] {
  const self = productBySlug(slug)
  return catalogue
    .filter((p) => p.category === self?.category && p.slug !== slug && p.sizes.length === 0 && p.stock > 0)
    .sort((a, b) => compareRating(b, a))
    .slice(0, n)
    .map((p) => card(p, l))
}
