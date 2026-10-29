// The order domain, ported from examples/shop/app/projects/project+/orders/order.go: the order list
// of a project, order details and the storage behind them. The orders are kept in memory, as in Go,
// and start with the demonstration data. Every query filters the orders of a project, copies them
// and sorts them, as ordersOf does there, and computes what it returns from that, with nothing kept
// from an earlier request.
import { counts } from './counts.ts'
import { demoOrders } from './demo.ts'
import { carrierName } from './shipping.ts'
import { NotFound } from './errors.ts'
import { initials, itemCount, status, total } from './records.ts'
import type { OrderRecord } from './records.ts'
import { logOf, packing, payment } from './detail.ts'
import type { LogEntry, PackLine, Payment } from './detail.ts'
import { rfc3339 } from './time.ts'

export interface LineItem {
  slug: string
  name: string
  description: string
  quantity: number
  priceCents: number
  color: string
  stock: number
  demand: number
  sold: number
}

export interface Shipment {
  carrier: string
  carrierName: string
  trackingNo: string
  shippedAt: string
  events: { at: string; place: string; note: string }[]
}

export interface Order {
  id: string
  projectId: string
  customer: string
  initials: string
  phone: string
  email: string
  address: string
  note: string
  placedAt: string
  status: string
  totalCents: number
  items: LineItem[]
  shipment: Shipment | null
  payment: Payment
  log: LogEntry[]
  notes: { author: string; text: string; at: string }[] | null
  packing: PackLine[]
}

export interface OrderRow {
  id: string
  customer: string
  initials: string
  placedAt: string
  status: string
  items: number // pieces in the order
  lines: number // different products in the order
  product: string
  slug: string
  carrier: string // the name of the carrier, empty until the order ships
  trackingNo: string
  totalCents: number
}

export interface OrderList {
  orders: OrderRow[]
  revenueCents: number
  pending: number
  shipped: number
}

// The orders, made when the process starts.
const orders: OrderRecord[] = demoOrders()

// ordersOf returns copies of the orders of a project, newest first.
export function ordersOf(projectId: string): OrderRecord[] {
  counts.ordersOf++
  const list: OrderRecord[] = []
  for (const o of orders) {
    if (o.projectId === projectId) list.push({ ...o, items: o.items.slice() })
  }
  list.sort((a, b) => b.placedAt - a.placedAt || (a.id < b.id ? 1 : a.id > b.id ? -1 : 0))
  return list
}

// orderRow is the line of the order list for an order.
export function orderRow(o: OrderRecord): OrderRow {
  const row: OrderRow = {
    id: o.id, customer: o.customer, initials: initials(o.customer), placedAt: rfc3339(o.placedAt), status: status(o),
    items: itemCount(o), lines: o.items.length, product: '', slug: '', carrier: '', trackingNo: '', totalCents: total(o),
  }
  if (o.items.length > 0) {
    row.product = o.items[0].name
    row.slug = o.items[0].slug
  }
  if (o.shipment) {
    row.carrier = carrierName(o.shipment.carrier)
    row.trackingNo = o.shipment.trackingNo
  }
  return row
}

// listOrders returns the orders of a project, newest first, with their totals.
export function listOrders(projectId: string): OrderList {
  const l: OrderList = { orders: [], revenueCents: 0, pending: 0, shipped: 0 }
  for (const o of ordersOf(projectId)) {
    const row = orderRow(o)
    l.orders.push(row)
    l.revenueCents += row.totalCents
    if (o.shipment) l.shipped++
    else l.pending++
  }
  return l
}

// getOrder returns the order id of a project, or throws NotFound.
export function getOrder(projectId: string, id: string): Order {
  const o = orders.find((o) => o.projectId === projectId && o.id === id)
  if (!o) throw new NotFound(id)
  return view(o)
}

// countPending returns how many orders of a project wait to be shipped.
export function countPending(projectId: string): number {
  let n = 0
  for (const o of ordersOf(projectId)) if (!o.shipment) n++
  return n
}

function view(o: OrderRecord): Order {
  const order: Order = {
    id: o.id, projectId: o.projectId, customer: o.customer, initials: initials(o.customer),
    phone: o.phone, email: o.email, address: o.address, note: o.note, placedAt: rfc3339(o.placedAt), status: status(o),
    totalCents: total(o), items: [], shipment: null, payment: payment(o), log: logOf(o), packing: packing(o),
    notes: o.notes ? o.notes.map((n) => ({ author: n.author, text: n.text, at: rfc3339(n.at) })) : null,
  }
  const s = o.shipment
  if (s) {
    order.shipment = {
      carrier: s.carrier, carrierName: carrierName(s.carrier), trackingNo: s.trackingNo, shippedAt: rfc3339(s.shippedAt),
      events: s.events.map((e) => ({ at: rfc3339(e.at), place: e.place, note: e.note })),
    }
  }
  for (const item of o.items) {
    order.items.push({
      slug: item.slug, name: item.name, description: item.description, quantity: item.quantity,
      priceCents: item.priceCents, color: item.color, stock: item.stock, demand: waitingFor(o.projectId, item.slug),
      sold: soldOf(o.projectId, item.slug),
    })
  }
  return order
}

// customerOrders returns the other orders of the customer of order id in a project, newest first,
// with the number of orders and the cents they spent in all, this one counted.
export function customerOrders(projectId: string, id: string): { others: OrderRow[]; count: number; spentCents: number } {
  let o: Order
  try {
    o = getOrder(projectId, id)
  } catch {
    return { others: [], count: 0, spentCents: 0 }
  }
  const others: OrderRow[] = []
  let count = 0
  let spentCents = 0
  for (const row of listOrders(projectId).orders) {
    if (row.customer !== o.customer) continue
    count++
    spentCents += row.totalCents
    if (row.id !== id) others.push(row)
  }
  return { others, count, spentCents }
}

// soldOf counts the pieces of a product in the orders of a project.
function soldOf(projectId: string, slug: string): number {
  let n = 0
  for (const o of orders) {
    if (o.projectId !== projectId) continue
    for (const item of o.items) if (item.slug === slug) n += item.quantity
  }
  return n
}

// waitingFor counts the pieces of a product in the orders of a project that wait to be shipped.
function waitingFor(projectId: string, slug: string): number {
  let n = 0
  for (const o of orders) {
    if (o.projectId !== projectId || o.shipment) continue
    for (const item of o.items) if (item.slug === slug) n += item.quantity
  }
  return n
}

// ordersWith returns up to n other orders of a project that hold the product slug, newest first.
export function ordersWith(projectId: string, slug: string, exceptId: string, n: number): OrderRow[] {
  const rows: OrderRow[] = []
  for (const row of listOrders(projectId).orders) {
    if (row.id === exceptId) continue
    let o: Order
    try {
      o = getOrder(projectId, row.id)
    } catch {
      continue
    }
    if (!o.items.some((i) => i.slug === slug)) continue
    rows.push(row)
    if (rows.length === n) break
  }
  return rows
}
