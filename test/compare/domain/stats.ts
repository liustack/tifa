// Sales of a shop over the last thirty days, ported from
// examples/shop/app/projects/project+/orders/stats.go, for the stats page.
import { ordersOf, orderRow } from './orders.ts'
import { initials, total } from './records.ts'
import type { OrderRow } from './orders.ts'
import { carriers } from './shipping.ts'
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
  name: string
  quantity: number
  revenueCents: number
}

export interface ProductRow {
  slug: string
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

const dispatchLimits = [6 * HOUR, 12 * HOUR, 24 * HOUR]

const dispatchBands = (): DispatchBand[] => [
  { label: '6 小时内', orders: 0 }, { label: '6 到 12 小时', orders: 0 }, { label: '12 到 24 小时', orders: 0 }, { label: '超过 24 小时', orders: 0 },
]

function dispatchBand(d: number): number {
  for (let i = 0; i < dispatchLimits.length; i++) if (d < dispatchLimits[i]) return i
  return dispatchLimits.length
}

const byText = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0)

// salesStats returns the sales of a project over the thirty days that end on the day of its newest
// order, with days starting at midnight in zone, the time zone of the shop. A project without
// orders has no days.
export function salesStats(projectId: string, zone: string): Sales {
  const s: Sales = {
    days: [], topProducts: [], products: [], carriers: carrierStats(projectId), customers: [],
    heat: Array.from({ length: 7 }, () => new Array<number>(12).fill(0)), dispatch: dispatchBands(), recent: [],
    totals: { pieces: 0, pendingOrders: 0, pendingCents: 0, avgDispatchHours: 0, customers: 0, repeat: 0 },
  }
  const list = ordersOf(projectId) // newest first
  if (list.length === 0) return s
  // An order belongs to the day of its time in zone. The dates themselves are only labels of the
  // calendar, counted in UTC.
  const last = local(list[0].placedAt, zone)
  const first = Date.UTC(last.year, last.month - 1, last.day - (statsDays - 1))
  const index = new Map<string, number>()
  for (let i = 0; i < statsDays; i++) {
    const date = new Date(first + i * 24 * HOUR).toISOString().slice(0, 10)
    index.set(date, i)
    s.days.push({ date, orders: 0, revenueCents: 0, shipped: 0, pieces: 0 })
  }
  const products = new Map<string, ProductSales>()
  const rows = new Map<string, ProductRow>()
  const customers = new Map<string, CustomerSales>()
  let dispatched = 0
  for (const o of list) {
    const l = local(o.placedAt, zone)
    s.heat[(l.weekday + 6) % 7][Math.floor(l.hour / 2)]++
    let c = customers.get(o.customer)
    if (!c) {
      c = { name: o.customer, initials: initials(o.customer), orders: 0, spentCents: 0 }
      customers.set(o.customer, c)
    }
    c.orders++
    c.spentCents += total(o)
    const sh = o.shipment
    if (sh) {
      const h = sh.shippedAt - o.placedAt
      dispatched += h
      s.dispatch[dispatchBand(h)].orders++
    } else {
      s.totals.pendingOrders++
      s.totals.pendingCents += total(o)
    }
    const date = `${l.year}-${String(l.month).padStart(2, '0')}-${String(l.day).padStart(2, '0')}`
    const i = index.get(date)
    if (i === undefined) continue
    const d = s.days[i]
    d.orders++
    d.revenueCents += total(o)
    if (o.shipment) d.shipped++
    for (const item of o.items) {
      let p = products.get(item.name)
      if (!p) {
        p = { slug: item.slug, name: item.name, quantity: 0, revenueCents: 0 }
        products.set(item.name, p)
        rows.set(item.name, { slug: item.slug, name: item.name, quantity: 0, orders: 0, revenueCents: 0, daily: new Array<number>(statsDays).fill(0) })
      }
      p.quantity += item.quantity
      p.revenueCents += item.quantity * item.priceCents
      const r = rows.get(item.name)!
      r.quantity += item.quantity
      r.orders++
      r.revenueCents += item.quantity * item.priceCents
      r.daily[i] += item.quantity
      d.pieces += item.quantity
      s.totals.pieces += item.quantity
    }
  }
  const shipped = list.length - s.totals.pendingOrders
  if (shipped > 0) s.totals.avgDispatchHours = hours(dispatched) / shipped
  for (const c of customers.values()) {
    s.customers.push(c)
    s.totals.customers++
    if (c.orders > 1) s.totals.repeat++
  }
  s.customers.sort((a, b) => b.spentCents - a.spentCents || byText(a.name, b.name))
  s.customers = s.customers.slice(0, 8)
  for (const r of rows.values()) s.products.push(r)
  s.products.sort((a, b) => b.quantity - a.quantity || b.revenueCents - a.revenueCents || byText(a.name, b.name))
  for (const o of list.slice(0, 8)) s.recent.push(orderRow(o))
  for (const p of products.values()) s.topProducts.push(p)
  s.topProducts.sort((a, b) => b.quantity - a.quantity || b.revenueCents - a.revenueCents || byText(a.name, b.name))
  if (s.topProducts.length > 5) s.topProducts = s.topProducts.slice(0, 5)
  return s
}

// carrierStats returns a line for each carrier, in the order of carriers. Transit counts only the
// parcels that have been signed for.
export function carrierStats(projectId: string): CarrierSales[] {
  const out: CarrierSales[] = carriers.map((c) => ({ code: c.code, name: c.name, orders: 0, avgDispatchHours: 0, avgTransitHours: 0 }))
  const dispatch = carriers.map(() => 0)
  const transit = carriers.map(() => 0)
  const delivered = carriers.map(() => 0)
  for (const o of ordersOf(projectId)) {
    const s = o.shipment
    const i = s ? carriers.findIndex((c) => c.code === s.carrier) : -1
    if (!s || i < 0) continue
    out[i].orders++
    dispatch[i] += s.shippedAt - o.placedAt
    const n = s.events.length
    if (n > 0 && s.events[n - 1].note.startsWith('已签收')) {
      delivered[i]++
      transit[i] += s.events[n - 1].at - s.shippedAt
    }
  }
  for (let i = 0; i < out.length; i++) {
    if (out[i].orders > 0) out[i].avgDispatchHours = hours(dispatch[i]) / out[i].orders
    if (delivered[i] > 0) out[i].avgTransitHours = hours(transit[i]) / delivered[i]
  }
  return out
}
