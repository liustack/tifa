// Ported from products/menu.go: the category menu and the products bought together.
import { inLocale } from '../store.ts'
import type { Locale } from '../store.ts'
import { catalogue, categories } from './catalogue.ts'
import { card, compareRating, productBySlug } from './product.ts'
import type { Category } from './product.ts'
import { kinds } from './kinds.ts'

// MenuGroup is one category of the store's category menu: its kinds of products, how many
// products it has, and the products the menu shows beside them.
export interface MenuGroup extends Category {
  count: number
  kinds: string[]
  featured: MenuProduct[]
}

// MenuProduct is a product as the menu shows it beside a category: its photo, number, name and
// price, and nothing of its stock.
export interface MenuProduct {
  slug: string
  photo: string
  name: string
  category: string
  priceCents: number
  compareAtCents: number
  number: number
}

// menu returns the category menu: each category with its kinds and its two best rated products.
export function menu(l: Locale): MenuGroup[] {
  return categories.map((c) => {
    const inCategory = catalogue.filter((p) => p.category === c.id)
    const k = (kinds[c.id] ?? []).map((x) => inLocale(x, l))
    const best = [...inCategory].sort((a, b) => compareRating(b, a)).slice(0, 2).map((p): MenuProduct => {
      const c = card(p, l)
      return { slug: c.slug, photo: c.photo, name: c.name, category: c.category, priceCents: c.priceCents, compareAtCents: c.compareAtCents, number: c.number }
    })
    return { id: c.id, name: inLocale(c.name, l), count: inCategory.length, kinds: k, featured: best }
  })
}

// BundleItem is a product the page of another offers to buy with it: its photo, number, name and
// price, and color, the ID of the colour it goes into the cart in.
export interface BundleItem {
  slug: string
  photo: string
  name: string
  priceCents: number
  number: number
  color: string
}

// bundle returns up to n products to buy together with slug: others of its category that come in
// one size, best rated first, so each goes into the cart in its first colour.
export function bundle(slug: string, n: number, l: Locale): BundleItem[] {
  const self = productBySlug(slug)
  return catalogue
    .filter((p) => p.category === self?.category && p.slug !== slug && p.sizes.length === 0 && p.stock > 0)
    .sort((a, b) => compareRating(b, a))
    .slice(0, n)
    .map((p) => {
      const c = card(p, l)
      return { slug: c.slug, photo: c.photo, name: c.name, priceCents: c.priceCents, number: c.number, color: p.colors[0] }
    })
}
