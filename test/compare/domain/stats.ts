// Sales of a shop over the last thirty days, ported from
// examples/shop/app/projects/project+/orders/stats.go, for the stats page.
import { newestFirst } from './orders.ts'
import { initials } from './records.ts'
import type { OrderRow } from './orders.ts'
import { carriers } from './shipping.ts'
import { byText, daysIn, ordersOf, productPhoto, shops } from './storage.ts'
import type { ProjectOrders } from './storage.ts'
import { HOUR, hours, local } from './time.ts'

// statsDays is how many days the stats page covers.
const statsDays = 30

export interface DaySales {
  date: string // 2026-10-13
  orders: number
  revenueCents: number
  shipped: number
  pieces: number
}

export interface ProductSales {
  slug: string
  photo: string
  name: string
  quantity: number
  revenueCents: number
}

export interface ProductRow {
  slug: string
  photo: string
  name: string
  quantity: number
  orders: number
  revenueCents: number
  daily: number[]
}

export interface CustomerSales {
  name: string
  initials: string
  orders: number
  spentCents: number
}

export interface DispatchBand {
  label: string
  orders: number
}

export interface CarrierSales {
  code: string
  name: string
  orders: number
  avgDispatchHours: number
  avgTransitHours: number
}

export interface Sales {
  days: DaySales[]
  topProducts: ProductSales[]
  products: ProductRow[]
  carriers: CarrierSales[]
  customers: CustomerSales[]
  heat: number[][]
  dispatch: DispatchBand[]
  recent: OrderRow[]
  totals: {
    pieces: number
    pendingOrders: number
    pendingCents: number
    avgDispatchHours: number
    customers: number
    repeat: number
  }
}

const dispatchBands = (): DispatchBand[] => [
  { label: 'Within 6 hours', orders: 0 }, { label: '6 to 12 hours', orders: 0 }, { label: '12 to 24 hours', orders: 0 }, { label: 'Over 24 hours', orders: 0 },
]

// salesStats returns the sales of a project over the thirty days that end on the day of its newest
// order, with days starting at midnight in zone, the time zone of the shop. A project without
// orders has no days. Everything comes from the sums the writes keep, as in Go.
// productName is the name of the product slug in the shop of a project as the shop has it now, as
// productName in create.go.
function productName(projectId: string, slug: string): string {
  const name = shops[projectId].name(slug)
  if (name === undefined) throw new Error(`orders of ${projectId} hold ${slug}, which the shop does not sell`)
  return name
}

export function salesStats(projectId: string, zone: string): Sales {
  const p = ordersOf(projectId)
  const s: Sales = {
    days: [], topProducts: [], products: [], carriers: carrierStats(p), customers: [],
    heat: Array.from({ length: 7 }, () => new Array<number>(12).fill(0)), dispatch: dispatchBands(), recent: [],
    totals: { pieces: 0, pendingOrders: 0, pendingCents: 0, avgDispatchHours: 0, customers: 0, repeat: 0 },
  }
  if (p.all.length === 0) return s
  const z = daysIn(p, zone)
  s.heat = z.heat.map((row) => row.slice())
  // An order belongs to the day of its time in zone. The dates themselves are only labels of the
  // calendar, counted in UTC.
  const last = local(p.all[p.all.length - 1].placedAt, zone)
  const first = Date.UTC(last.year, last.month - 1, last.day - (statsDays - 1))
  const products = new Map<string, ProductRow>()
  for (let i = 0; i < statsDays; i++) {
    const date = new Date(first + i * 24 * HOUR).toISOString().slice(0, 10)
    const day: DaySales = { date, orders: 0, revenueCents: 0, shipped: 0, pieces: 0 }
    const d = z.days.get(date)
    if (d) {
      day.orders = d.orders
      day.revenueCents = d.revenueCents
      day.shipped = d.shipped
      day.pieces = d.pieces
      s.totals.pieces += d.pieces
      for (const [slug, dp] of d.products) {
        let r = products.get(slug)
        if (!r) {
          r = { slug, photo: productPhoto(projectId, slug), name: productName(projectId, slug), quantity: 0, orders: 0, revenueCents: 0, daily: new Array<number>(statsDays).fill(0) }
          products.set(slug, r)
        }
        r.quantity += dp.quantity
        r.orders += dp.orders
        r.revenueCents += dp.revenueCents
        r.daily[i] += dp.quantity
      }
    }
    s.days.push(day)
  }
  for (const r of products.values()) {
    s.products.push(r)
    s.topProducts.push({ slug: r.slug, photo: r.photo, name: r.name, quantity: r.quantity, revenueCents: r.revenueCents })
  }
  s.products.sort((a, b) => b.quantity - a.quantity || b.revenueCents - a.revenueCents || byText(a.name, b.name) || byText(a.slug, b.slug))
  s.topProducts.sort((a, b) => b.quantity - a.quantity || b.revenueCents - a.revenueCents || byText(a.name, b.name) || byText(a.slug, b.slug))
  s.topProducts = s.topProducts.slice(0, 5)
  s.customers = p.ranked.slice(0, 8).map((c) => ({ name: c.name, initials: initials(c.name), orders: c.orders.length, spentCents: c.spentCents }))
  s.totals.customers = p.customers.size
  s.totals.repeat = p.repeat
  s.totals.pendingOrders = p.pending.length
  s.totals.pendingCents = p.pendingCents
  const shipped = p.all.length - p.pending.length
  if (shipped > 0) s.totals.avgDispatchHours = hours(p.dispatched) / shipped
  p.bands.forEach((n, i) => (s.dispatch[i].orders = n))
  s.recent = newestFirst(p.all, 0, 8, '')
  return s
}

// carrierStats returns a line for each carrier, in the order of carriers. Transit counts only the
// parcels that have been signed for.
function carrierStats(p: ProjectOrders): CarrierSales[] {
  return carriers.map((c, i) => {
    const t = p.carriers[i]
    return {
      code: c.code, name: c.name, orders: t.orders,
      avgDispatchHours: t.orders > 0 ? hours(t.dispatch) / t.orders : 0,
      avgTransitHours: t.delivered > 0 ? hours(t.transit) / t.delivered : 0,
    }
  })
}
