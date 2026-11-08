// The orders kept in memory with what the pages read, ported from
// examples/shop/app/projects/project+/orders/storage.go. Beside the orders, each project keeps its
// orders by time, by number, by customer and by product, and the sums the order list, the stats
// and the order page show. Every write updates them with the order, so a read of the pages costs
// about the same with 50 orders as with 5000. They start with the demonstration data.
import { counts } from './counts.ts'
import { demoOrders, weekendCatalogue } from './demo.ts'
import { Invalid, NotFound } from './errors.ts'
import { Fields } from './store/errors.ts'
import { orderLine } from './store/products/edit.ts'
import { storefrontProject } from './store/store.ts'
import { carrierName, carriers } from './shipping.ts'
import { total } from './records.ts'
import type { ItemRecord, OrderRecord, ShipmentRecord } from './records.ts'
import { HOUR, local } from './time.ts'

// A NewLine is one product of a new order in a colour and size, as in create.go. Staff leave the
// colour and size empty.
export interface NewLine {
  slug: string
  color: string
  size: string
  quantity: number
}

// storefrontLine is the order line of a product of the storefront, which the studio shop sells.
function storefrontLine(l: NewLine): ItemRecord {
  let p
  try {
    p = orderLine(l.slug, l.color, l.size)
  } catch (e) {
    if (e instanceof NotFound) throw new Invalid(['items'])
    if (e instanceof Fields) throw new Invalid(Object.keys(e.fields))
    throw e
  }
  return { slug: p.slug, name: p.name, description: p.variant, quantity: l.quantity, priceCents: p.priceFen, color: p.color, stock: p.stock }
}

// weekendLine is the order line of a product of the weekend shop, which comes in one colour and size.
function weekendLine(l: NewLine): ItemRecord {
  const p = weekendCatalogue.find((p) => p.slug === l.slug)
  if (!p || l.color !== '' || l.size !== '') throw new Invalid(['items'])
  return { ...p, quantity: l.quantity }
}

// The form of the order numbers of a project and the line of each product it sells, as shops in
// create.go: the studio shop sells the products of the storefront, the weekend shop has its own.
export const shops: Record<string, { prefix: string; width: number; line: (l: NewLine) => ItemRecord }> = {
  [storefrontProject]: { prefix: 'SH', width: 0, line: storefrontLine },
  weekend: { prefix: 'WK', width: 4, line: weekendLine },
}

export interface CustomerOrders {
  name: string
  orders: OrderRecord[]
  spentCents: number
}

export interface ProductOrders {
  orders: OrderRecord[] // the orders that hold the product
  sold: number // pieces in all the orders
  waiting: number // pieces in the orders waiting to ship
}

export interface CarrierTotals {
  orders: number
  dispatch: number // from order to shipping, added up
  delivered: number // parcels signed for
  transit: number // from shipping to signing, added up over the parcels signed for
}

// DayProduct is what the orders of a day hold of one product. A product's name and slug go
// together, as in the catalogues.
export interface DayProduct {
  slug: string
  quantity: number
  lines: number
  revenueCents: number
}

export interface DayTotals {
  orders: number
  revenueCents: number
  pending: number
  shipped: number
  pieces: number
  products: Map<string, DayProduct> // by name
}

// ZoneDays are the orders of a project counted by the day they were placed on and the hour of the
// week, in one time zone.
export interface ZoneDays {
  zone: string
  days: Map<string, DayTotals> // by date in the zone, 2026-10-13
  heat: number[][] // as Sales.heat
}

// ProjectOrders are the orders of one project with what its pages read. Lists of orders are oldest
// first, by the time the order was placed and then by its number (see byTime), so that a new
// order, the newest, goes at the end.
export interface ProjectOrders {
  byId: Map<string, OrderRecord>
  all: OrderRecord[]
  pending: OrderRecord[] // the orders waiting to ship
  last: number // the highest number of the order numbers, which the next order follows
  revenueCents: number
  pendingCents: number
  customers: Map<string, CustomerOrders> // by name
  ranked: CustomerOrders[] // by the cents spent, most first, then by name
  repeat: number // customers with more than one order
  products: Map<string, ProductOrders> // by slug
  carriers: CarrierTotals[] // in the order of carriers
  shippedBy: Map<string, number> // how many orders went with each carrier, by its name
  dispatched: number // from order to shipping, added over the shipped orders
  bands: number[] // the shipped orders in each band of dispatchBands
  zones: Map<string, ZoneDays> // made on the first read in a zone and kept up by every write after it
}

// dispatchLimits are the times within which orders are handed to the carrier.
export const dispatchLimits = [6 * HOUR, 12 * HOUR, 24 * HOUR]

export function dispatchBand(d: number): number {
  for (let i = 0; i < dispatchLimits.length; i++) if (d < dispatchLimits[i]) return i
  return dispatchLimits.length
}

function newProjectOrders(): ProjectOrders {
  return {
    byId: new Map(), all: [], pending: [], last: 0, revenueCents: 0, pendingCents: 0, customers: new Map(), ranked: [], repeat: 0,
    products: new Map(), carriers: carriers.map(() => ({ orders: 0, dispatch: 0, delivered: 0, transit: 0 })), shippedBy: new Map(),
    dispatched: 0, bands: new Array<number>(dispatchLimits.length + 1).fill(0), zones: new Map(),
  }
}

// The orders, made when the process starts. They live on globalThis, like the counters, since Next.js
// loads this module once for its pages and once for the write endpoint, and both must see the same orders.
const g = globalThis as typeof globalThis & { __compareOrders?: Map<string, ProjectOrders> }
export const orders: Map<string, ProjectOrders> = (g.__compareOrders ??= load(demoOrders()))

// noOrders is what ordersOf returns for a project without orders. Reads only read it.
const noOrders = newProjectOrders()

// ordersOf returns the orders of a project with what its pages read. Every read of the orders of a
// project starts with it, and calls it once, as in Go.
export function ordersOf(projectId: string): ProjectOrders {
  counts.ordersOf++
  return orders.get(projectId) ?? noOrders
}

// findOrder returns the order id of a project and the orders of its project, or undefined, for a write.
export function findOrder(projectId: string, id: string): [ProjectOrders, OrderRecord] | undefined {
  const p = orders.get(projectId)
  const o = p?.byId.get(id)
  return p && o ? [p, o] : undefined
}

function load(list: OrderRecord[]): Map<string, ProjectOrders> {
  const m = new Map<string, ProjectOrders>()
  for (const o of list) insert(m, o)
  return m
}

// insert keeps a new order in m.
export function insert(m: Map<string, ProjectOrders>, o: OrderRecord): void {
  let p = m.get(o.projectId)
  if (!p) {
    p = newProjectOrders()
    m.set(o.projectId, p)
  }
  const s = shops[o.projectId]
  if (s) {
    const n = Number(o.id.slice(s.prefix.length + 1))
    if (Number.isInteger(n)) p.last = Math.max(p.last, n)
  }
  p.byId.set(o.id, o)
  place(p.all, o, byTime)
  p.revenueCents += total(o)
  if (!o.shipment) {
    place(p.pending, o, byTime)
    p.pendingCents += total(o)
  } else {
    addShipment(p, o)
  }
  join(p, o)
  const seen = new Set<string>()
  for (const item of o.items) {
    let pr = p.products.get(item.slug)
    if (!pr) {
      pr = { orders: [], sold: 0, waiting: 0 }
      p.products.set(item.slug, pr)
    }
    pr.sold += item.quantity
    if (!o.shipment) pr.waiting += item.quantity
    if (!seen.has(item.slug)) {
      seen.add(item.slug)
      place(pr.orders, o, byTime)
    }
  }
  for (const z of p.zones.values()) addToZone(z, o)
}

// ship records the shipment of an order waiting to ship.
export function ship(p: ProjectOrders, o: OrderRecord, s: ShipmentRecord): void {
  remove(p.pending, o, byTime)
  p.pendingCents -= total(o)
  for (const item of o.items) p.products.get(item.slug)!.waiting -= item.quantity
  o.shipment = s
  addShipment(p, o)
  for (const z of p.zones.values()) {
    const d = z.days.get(local(o.placedAt, z.zone).date)!
    d.pending--
    d.shipped++
  }
}

// rename gives an order another customer.
export function rename(p: ProjectOrders, o: OrderRecord, customer: string): void {
  leave(p, o)
  o.customer = customer
  join(p, o)
}

function addShipment(p: ProjectOrders, o: OrderRecord): void {
  const s = o.shipment!
  const name = carrierName(s.carrier)
  p.shippedBy.set(name, (p.shippedBy.get(name) ?? 0) + 1)
  const h = s.shippedAt - o.placedAt
  p.dispatched += h
  p.bands[dispatchBand(h)]++
  const i = carriers.findIndex((c) => c.code === s.carrier)
  if (i < 0) return
  const c = p.carriers[i]
  c.orders++
  c.dispatch += h
  const n = s.events.length
  if (n > 0 && s.events[n - 1].note.startsWith('Delivered')) {
    c.delivered++
    c.transit += s.events[n - 1].at - s.shippedAt
  }
}

function join(p: ProjectOrders, o: OrderRecord): void {
  let c = p.customers.get(o.customer)
  if (!c) {
    c = { name: o.customer, orders: [], spentCents: 0 }
    p.customers.set(o.customer, c)
  } else {
    remove(p.ranked, c, byRank)
  }
  place(c.orders, o, byTime)
  c.spentCents += total(o)
  if (c.orders.length === 2) p.repeat++
  place(p.ranked, c, byRank)
}

function leave(p: ProjectOrders, o: OrderRecord): void {
  const c = p.customers.get(o.customer)!
  remove(p.ranked, c, byRank)
  if (c.orders.length === 2) p.repeat--
  remove(c.orders, o, byTime)
  c.spentCents -= total(o)
  if (c.orders.length === 0) {
    p.customers.delete(o.customer)
    return
  }
  place(p.ranked, c, byRank)
}

// daysIn returns the orders of p counted by day in zone. The first read in a zone counts them, and
// the writes after it keep the count. zone is an IANA name, which is all Intl needs to know of it.
export function daysIn(p: ProjectOrders, zone: string): ZoneDays {
  let z = p.zones.get(zone)
  if (!z) {
    z = { zone, days: new Map(), heat: Array.from({ length: 7 }, () => new Array<number>(12).fill(0)) }
    for (const o of p.all) addToZone(z, o)
    p.zones.set(zone, z)
  }
  return z
}

function addToZone(z: ZoneDays, o: OrderRecord): void {
  const l = local(o.placedAt, z.zone)
  z.heat[(l.weekday + 6) % 7][Math.floor(l.hour / 2)]++
  let d = z.days.get(l.date)
  if (!d) {
    d = { orders: 0, revenueCents: 0, pending: 0, shipped: 0, pieces: 0, products: new Map() }
    z.days.set(l.date, d)
  }
  d.orders++
  d.revenueCents += total(o)
  if (o.shipment) d.shipped++
  else d.pending++
  for (const item of o.items) {
    let pr = d.products.get(item.name)
    if (!pr) {
      pr = { slug: item.slug, quantity: 0, lines: 0, revenueCents: 0 }
      d.products.set(item.name, pr)
    }
    pr.quantity += item.quantity
    pr.lines++
    pr.revenueCents += item.quantity * item.priceCents
    d.pieces += item.quantity
  }
}

export function byText(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0
}

// byTime orders the orders of a project oldest first, by the time they were placed and then by
// number. The lists of the pages are newest first: this order backwards.
function byTime(a: OrderRecord, b: OrderRecord): number {
  return a.placedAt - b.placedAt || byText(a.id, b.id)
}

// byRank orders the customers by what they spent, most first, and then by name.
function byRank(a: CustomerOrders, b: CustomerOrders): number {
  return b.spentCents - a.spentCents || byText(a.name, b.name)
}

// where finds where v goes in list, sorted by by.
function where<T>(list: T[], v: T, by: (a: T, b: T) => number): number {
  let lo = 0
  let hi = list.length
  while (lo < hi) {
    const mid = (lo + hi) >>> 1
    if (by(list[mid], v) < 0) lo = mid + 1
    else hi = mid
  }
  return lo
}

// place puts v into list, sorted by by, where it belongs. The newest order goes at the end of a list
// by time, so placing it costs no more as the list grows.
function place<T>(list: T[], v: T, by: (a: T, b: T) => number): void {
  list.splice(where(list, v, by), 0, v)
}

// remove takes v out of list, sorted by by.
function remove<T>(list: T[], v: T, by: (a: T, b: T) => number): void {
  const i = where(list, v, by)
  if (list[i] !== v) throw new Error('orders: an index lost an entry')
  list.splice(i, 1)
}
