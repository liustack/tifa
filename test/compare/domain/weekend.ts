// The weekend shop and its catalogue, ported from orders/weekend.go: staff change its products with
// the same edit as those of the storefront, and every order takes its pieces from their stock.
import { weekendCatalogue, weekendStock } from './demo.ts'
import { Invalid, NotFound, OutOfStock } from './errors.ts'
import type { ItemRecord } from './records.ts'
import type { NewLine } from './storage.ts'
import { apply, entries, getEntry, over, trim } from './store/products/edit.ts'
import type { Change, Edit, Entry } from './store/products/edit.ts'
import { storefrontProject } from './store/store.ts'

interface WeekendProduct extends Edit {
  slug: string
  variant: string
  color: string
}

// newWeekendShelf is the weekend shop as the demonstration data starts it, as in Go.
function newWeekendShelf(): WeekendProduct[] {
  const texts: Record<string, [string, string, string]> = {
    'canvas-tote': ['周末帆布袋', '厚帆布托特包，装得下一天的出行。', 'A heavy canvas tote that holds a day out.'],
    'folding-stool': ['露营折叠凳', '铝合金折叠凳，收起来只有一本书大。', 'An aluminium stool that folds to the size of a book.'],
    'picnic-blanket': ['野餐垫', '防水底的格纹野餐垫。', 'A checked picnic blanket with a waterproof back.'],
    'insulated-bottle': ['保温水壶', '双层不锈钢，冷热都能保持 12 小时。', 'Double-walled steel that keeps a drink hot or cold for 12 hours.'],
    postcards: ['手绘明信片', '六张城市手绘明信片。', 'Six hand-drawn postcards of cities.'],
  }
  return weekendCatalogue.map((p) => {
    const t = texts[p.slug]
    return {
      priceCents: p.priceCents, stock: weekendStock[p.slug], nameEn: p.name, nameZh: t[0], descriptionEn: t[2], descriptionZh: t[1],
      slug: p.slug, variant: p.description, color: p.color,
    }
  })
}

// On globalThis for the same reason as the orders: every bundle of an app shares one shop.
const g = globalThis as typeof globalThis & { __compareWeekend?: WeekendProduct[] }
const shelf: WeekendProduct[] = (g.__compareWeekend ??= newWeekendShelf())

export function weekendEntries(): Entry[] {
  return shelf.map((p, i) => ({
    priceCents: p.priceCents, stock: p.stock, nameEn: p.nameEn, nameZh: p.nameZh, descriptionEn: p.descriptionEn, descriptionZh: p.descriptionZh,
    slug: p.slug, number: i + 1, category: '',
  }))
}

export function getWeekendEntry(slug: string): Entry {
  const e = weekendEntries().find((e) => e.slug === slug)
  if (!e) throw new NotFound(slug)
  return e
}

// updateWeekend changes the fields of a product of the weekend shop that a change has, as Go's
// UpdateWeekend does.
export function updateWeekend(slug: string, c: Change): void {
  const p = shelf.find((p) => p.slug === slug)
  if (!p) throw new NotFound(slug)
  const e = over(c, p)
  p.priceCents = e.priceCents
  p.stock = e.stock
  p.nameEn = trim(e.nameEn)
  p.nameZh = trim(e.nameZh)
  p.descriptionEn = trim(e.descriptionEn)
  p.descriptionZh = trim(e.descriptionZh)
}

// weekendBuy takes the pieces of the lines of a new order from the stock of the weekend shop, or
// none of them, as weekendBuy does in Go.
export function weekendBuy(lines: NewLine[]): ItemRecord[] {
  const want = new Map<WeekendProduct, number>()
  const out: ItemRecord[] = []
  for (const l of lines) {
    const p = shelf.find((p) => p.slug === l.slug)
    if (!p || l.color !== '' || l.size !== '') throw new Invalid(['items'])
    want.set(p, (want.get(p) ?? 0) + l.quantity)
    if (want.get(p)! > p.stock) throw new OutOfStock()
    out.push({ slug: p.slug, name: p.nameEn, description: p.variant, quantity: l.quantity, priceCents: p.priceCents, color: p.color })
  }
  for (const [p, n] of want) p.stock -= n
  return out
}

export function weekendStockOf(slug: string): number | undefined {
  return shelf.find((p) => p.slug === slug)?.stock
}

export function weekendName(slug: string): string | undefined {
  return shelf.find((p) => p.slug === slug)?.nameEn
}

// shopEntries, shopEntry and updateShopProduct are the products of the shop of a project, as in Go.
export function shopEntries(projectId: string): Entry[] {
  if (projectId === storefrontProject) return entries()
  if (projectId === 'weekend') return weekendEntries()
  throw new NotFound(projectId)
}

export function shopEntry(projectId: string, slug: string): Entry {
  if (projectId === storefrontProject) return getEntry(slug)
  if (projectId === 'weekend') return getWeekendEntry(slug)
  throw new NotFound(projectId)
}

export function updateShopProduct(projectId: string, slug: string, c: Change): void {
  if (projectId === storefrontProject) return apply(slug, c)
  if (projectId === 'weekend') return updateWeekend(slug, c)
  throw new NotFound(projectId)
}
