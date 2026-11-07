// The order domain, ported from examples/shop/app/projects/project+/orders/order.go: the order list
// of a project, order details and the storage behind them. The orders are kept in memory, as in Go,
// and start with the demonstration data. Every query filters the orders of a project, copies them
// and sorts them, as ordersOf does there, and computes what it returns from that, with nothing kept
// from an earlier request.
import { counts } from './counts.ts'
import { demoOrders, studioCatalogue, warehouse, weekendCatalogue } from './demo.ts'
import { carrierName, carriers } from './shipping.ts'
import { Invalid, Locked, NotFound } from './errors.ts'
import { initials, itemCount, status, total } from './records.ts'
import type { ItemRecord, OrderRecord } from './records.ts'
import { logOf, packing, payment } from './detail.ts'
import type { LogEntry, PackLine, Payment } from './detail.ts'
import { DAY, local, rfc3339 } from './time.ts'

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
  notes: { author: string; text: string; at: string }[]
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

// A page of the order list holds the newest ListPageSize orders, and another list on a page the
// newest SideListSize. The numbers over all the orders come with them, as in order.go.
export const ListPageSize = 25
export const SideListSize = 10
const listDays = 14

export interface DailyCards {
  revenueCents: number[]
  orders: number[]
  pending: number[]
  shipped: number[]
}

export interface OrderList {
  orders: OrderRow[] // the newest ListPageSize, newest first
  total: number
  revenueCents: number
  pending: number
  shipped: number
  newestAt: string // when the newest order was placed
  oldestPending: OrderRow | null // null when every order has shipped
  carriers: string[] // names, sorted
  daily: DailyCards
}

// The orders, made when the process starts. They live on globalThis, like the counters, since Next.js
// loads this module once for its pages and once for the write endpoint, and both must see the same orders.
const g = globalThis as typeof globalThis & { __compareOrders?: OrderRecord[] }
const orders: OrderRecord[] = (g.__compareOrders ??= demoOrders())

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

// allRows are the rows of all the orders of a project, newest first.
export function allRows(projectId: string): OrderRow[] {
  return ordersOf(projectId).map(orderRow)
}

// dateKey is the date of an instant in a time zone, such as 2026-10-13.
function dateKey(ms: number, timeZone: string): string {
  const d = local(ms, timeZone)
  return `${d.year}-${String(d.month).padStart(2, '0')}-${String(d.day).padStart(2, '0')}`
}

// listOrders returns the first page of the orders of a project, newest first, with the numbers over
// all of them, as ListOrders does. timeZone is the one of the shop, whose days the cards draw.
export function listOrders(projectId: string, timeZone: string): OrderList {
  const l: OrderList = {
    orders: [], total: 0, revenueCents: 0, pending: 0, shipped: 0, newestAt: '0001-01-01T00:00:00Z', oldestPending: null, carriers: [],
    daily: { revenueCents: [], orders: [], pending: [], shipped: [] },
  }
  const list = ordersOf(projectId)
  if (list.length === 0) return l
  l.newestAt = rfc3339(list[0].placedAt)
  // The days, labelled by their date in the time zone, counted back from the newest order in steps
  // of 24 hours: two steps can land on the same date, and then they share the sums.
  const keys = Array.from({ length: listDays }, (_, i) => dateKey(list[0].placedAt - (listDays - 1 - i) * DAY, timeZone))
  const sums = new Map<string, [number, number, number, number]>(keys.map((k) => [k, [0, 0, 0, 0]]))
  const carriers = new Set<string>()
  let oldest: OrderRecord | undefined
  for (const o of list) {
    const cents = total(o)
    l.total++
    l.revenueCents += cents
    if (o.shipment) {
      l.shipped++
      carriers.add(carrierName(o.shipment.carrier))
    } else {
      l.pending++
      if (!oldest || o.placedAt < oldest.placedAt) oldest = o
    }
    const s = sums.get(dateKey(o.placedAt, timeZone))
    if (s) {
      s[0] += cents
      s[1]++
      if (o.shipment) s[3]++
      else s[2]++
    }
  }
  for (const k of keys) {
    const s = sums.get(k)!
    l.daily.revenueCents.push(s[0])
    l.daily.orders.push(s[1])
    l.daily.pending.push(s[2])
    l.daily.shipped.push(s[3])
  }
  if (oldest) l.oldestPending = orderRow(oldest)
  l.carriers = [...carriers].sort()
  l.orders = list.slice(0, ListPageSize).map(orderRow)
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
    notes: (o.notes ?? []).map((n) => ({ author: n.author, text: n.text, at: rfc3339(n.at) })),
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

// customerOrders returns the newest SideListSize other orders of the customer of order id in a project, newest first,
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
  for (const row of allRows(projectId)) {
    if (row.customer !== o.customer) continue
    count++
    spentCents += row.totalCents
    if (row.id !== id && others.length < SideListSize) others.push(row)
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
  for (const o of ordersOf(projectId)) {
    if (o.id === exceptId || !o.items.some((i) => i.slug === slug)) continue
    rows.push(orderRow(o))
    if (rows.length === n) break
  }
  return rows
}

// normalPhone drops the spaces and dashes people type between the digits of a phone number.
const normalPhone = (phone: string): string => phone.replace(/[ -]/g, '')

// checkRecipient lists the fields of a recipient that the shop would refuse, as CheckRecipient does
// in shipping.go: no name, a phone number that is not 11 digits starting 1[3-9], no address or one
// longer than 100 characters.
export function checkRecipient(customer: string, phone: string, address: string): string[] {
  const fields: string[] = []
  if (customer.trim() === '') fields.push('customer')
  if (!/^1[3-9][0-9]{9}$/.test(normalPhone(phone))) fields.push('phone')
  const a = address.trim()
  if (a === '' || Array.from(a).length > 100) fields.push('address')
  return fields
}

// updateRecipient changes who receives the order and where, until it ships, as UpdateRecipient does
// in shipping.go, and returns the order.
export function updateRecipient(projectId: string, id: string, customer: string, phone: string, address: string): Order {
  const fields = checkRecipient(customer, phone, address)
  if (fields.length > 0) throw new Invalid(fields)
  const o = orders.find((o) => o.projectId === projectId && o.id === id)
  if (!o) throw new NotFound(id)
  if (o.shipment) throw new Locked(id)
  o.customer = customer.trim()
  o.phone = normalPhone(phone)
  o.address = address.trim()
  return view(o)
}

// The next functions are the writes of the handlers in shipping.go, detail.go and create.go.

const trackingNo = /^[A-Za-z0-9]{8,20}$/

// checkShipment lists the fields of a shipment that the shop would refuse, as CheckShipment does.
export function checkShipment(carrier: string, tracking: string): string[] {
  const fields: string[] = []
  if (!carriers.some((c) => c.code === carrier)) fields.push('carrier')
  if (!trackingNo.test(tracking.trim())) fields.push('trackingNo')
  return fields
}

// shipOrder records that the order left at at, as ShipOrder does: an order ships once, and the
// second time throws Locked. It returns the order.
export function shipOrder(projectId: string, id: string, carrier: string, tracking: string, at: number): Order {
  const fields = checkShipment(carrier, tracking)
  if (fields.length > 0) throw new Invalid(fields)
  const o = orders.find((o) => o.projectId === projectId && o.id === id)
  if (!o) throw new NotFound(id)
  if (o.shipment) throw new Locked(id)
  o.shipment = {
    carrier, trackingNo: tracking.trim(), shippedAt: at,
    events: [{ at, place: warehouse, note: 'Courier collected the parcel' }],
  }
  return view(o)
}

// checkNote lists the field of a note that the shop would refuse, as CheckNote does.
export function checkNote(text: string): string[] {
  const t = text.trim()
  return t === '' || Array.from(t).length > 200 ? ['text'] : []
}

// addNote puts a note by the owner on an order, shipped or not, as AddNote does, and returns the order.
export function addNote(projectId: string, id: string, text: string, at: number): Order {
  const fields = checkNote(text)
  if (fields.length > 0) throw new Invalid(fields)
  const o = orders.find((o) => o.projectId === projectId && o.id === id)
  if (!o) throw new NotFound(id)
  ;(o.notes ??= []).push({ author: 'Owner', text: text.trim(), at })
  return view(o)
}

export interface NewOrder {
  customer: string
  phone: string
  email: string
  address: string
  note: string
  items: { slug: string; quantity: number }[]
}

// The form of the order numbers of a project and its products, as shops in create.go.
const shops: Record<string, { prefix: string; width: number; catalogue: ItemRecord[] }> = {
  studio: { prefix: 'SH', width: 0, catalogue: studioCatalogue },
  weekend: { prefix: 'WK', width: 4, catalogue: weekendCatalogue },
}

// checkNewOrder lists the fields of a new order that the shop would refuse, as CheckNewOrder does.
export function checkNewOrder(o: NewOrder): string[] {
  const fields = checkRecipient(o.customer, o.phone, o.address)
  if (Array.from(o.note.trim()).length > 200) fields.push('note')
  if (o.items.length < 1 || o.items.length > 3 || o.items.some((l) => !Number.isInteger(l.quantity) || l.quantity < 1 || l.quantity > 5)) {
    fields.push('items')
  }
  return fields
}

// createOrder places an order in a project at at, as CreateOrder does: its number is the one after
// the highest of the project. It returns the new order.
export function createOrder(projectId: string, input: NewOrder, at: number): Order {
  const fields = checkNewOrder(input)
  if (fields.length > 0) throw new Invalid(fields)
  const s = shops[projectId]
  if (!s) throw new NotFound(projectId)
  const items: ItemRecord[] = []
  for (const l of input.items) {
    const p = s.catalogue.find((p) => p.slug === l.slug)
    if (!p) throw new Invalid(['items'])
    items.push({ ...p, quantity: l.quantity })
  }
  let last = 0
  for (const o of orders) {
    if (o.projectId !== projectId) continue
    const n = Number(o.id.slice(s.prefix.length + 1))
    if (Number.isInteger(n)) last = Math.max(last, n)
  }
  const o: OrderRecord = {
    id: `${s.prefix}-${String(last + 1).padStart(s.width, '0')}`, projectId, customer: input.customer.trim(),
    phone: normalPhone(input.phone), email: input.email.trim(), address: input.address.trim(), note: input.note.trim(),
    placedAt: at, shipment: null, items, notes: null,
  }
  orders.push(o)
  return view(o)
}
