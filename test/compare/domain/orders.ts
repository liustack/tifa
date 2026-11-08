// The order domain, ported from examples/shop/app/projects/project+/orders/order.go: the order list
// of a project and order details. The orders are kept in memory with the indexes and sums the
// pages read (storage.ts), as in Go, and start with the demonstration data.
import { warehouse } from './demo.ts'
import { carrierName, carriers } from './shipping.ts'
import { Invalid, Locked, NotFound } from './errors.ts'
import { initials, itemCount, status, total } from './records.ts'
import type { ItemRecord, OrderRecord } from './records.ts'
import { logOf, packing, payment } from './detail.ts'
import type { LogEntry, PackLine, Payment } from './detail.ts'
import { DAY, local, rfc3339 } from './time.ts'
import { daysIn, findOrder, insert, orders, ordersOf, productStock, rename, ship, shops, storefrontItem } from './storage.ts'
import { storefrontProject } from './store/store.ts'
import type { ProductLine } from './store/products/edit.ts'
import type { NewLine, ProjectOrders } from './storage.ts'

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

// ListQuery is what the order list is asked for: the filters and the page. A field left empty is no
// filter. The page reads it from the URL, and listOrders gives back the cleaned query in
// OrderList.filter.
export interface ListQuery {
  status: string // 'pending' or 'shipped'
  carrier: string // the name of a carrier
  period: string // '7' or '30': placed within that many days of the newest order
  amount: string // 'low' (under 200 yuan), 'mid' (200 to 500 yuan) or 'high' (over 500 yuan)
  q: string // part of an order number or of a customer's name, in any case
  sort: string // 'placedAt' or 'totalCents'
  dir: string // 'desc' or 'asc'
  page: number // from 1
}

export const noQuery: ListQuery = { status: '', carrier: '', period: '', amount: '', q: '', sort: '', dir: '', page: 0 }

export interface StatusCounts {
  all: number
  pending: number
  shipped: number
}

export interface OrderList {
  orders: OrderRow[] // the ListPageSize orders of the page, newest first
  filter: ListQuery // the query the page answers
  matching: number // how many orders the filters keep
  pages: number // how many pages they make
  statuses: StatusCounts // the tabs, over the orders that every filter but the status keeps
  total: number
  revenueCents: number
  pending: number
  shipped: number
  newestAt: string // when the newest order was placed
  oldestPending: OrderRow | null // null when every order has shipped
  carriers: string[] // names, sorted
  daily: DailyCards
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

// newestFirst returns the rows of up to n orders of list, a list by time, newest first, from the
// from-th newest on, leaving out the order skip.
export function newestFirst(list: OrderRecord[], from: number, n: number, skip: string): OrderRow[] {
  const rows: OrderRow[] = []
  for (let i = list.length - 1 - from; i >= 0 && rows.length < n; i--) {
    if (list[i].id !== skip) rows.push(orderRow(list[i]))
  }
  return rows
}

// allRows are the rows of all the orders of a project, newest first.
export function allRows(projectId: string): OrderRow[] {
  const p = ordersOf(projectId)
  return newestFirst(p.all, 0, p.all.length, '')
}

// amountRanges are the choices of the amount filter, in cents, both ends included.
const amountRanges: Record<string, [number, number]> = {
  low: [0, 19999],
  mid: [20000, 50000],
  high: [50001, Infinity],
}

// cleanQuery leaves out what the URL says that is not one of the choices, and trims the search.
function cleanQuery(q: ListQuery): ListQuery {
  return {
    status: q.status === 'pending' || q.status === 'shipped' ? q.status : '',
    carrier: q.carrier,
    period: q.period === '7' || q.period === '30' ? q.period : '',
    amount: Object.hasOwn(amountRanges, q.amount) ? q.amount : '',
    q: q.q.replace(/^[ \t\n\v\f\r]+|[ \t\n\v\f\r]+$/g, ''), // ASCII white space only, as searchSpace in order.go
    sort: q.sort === 'totalCents' ? 'totalCents' : 'placedAt',
    dir: q.dir === 'asc' ? 'asc' : 'desc',
    page: Math.max(q.page, 1),
  }
}

// keeper returns the test for the filters other than the status. newest is when the newest order
// of the project was placed, which the period counts back from.
function keeper(q: ListQuery, newest: number): (r: OrderRow) => boolean {
  const needle = q.q.toLowerCase()
  const since = q.period ? newest - Number(q.period) * DAY : 0
  const range = amountRanges[q.amount]
  return (r) => {
    if (q.carrier !== '' && r.carrier !== q.carrier) return false
    if (q.period !== '' && Date.parse(r.placedAt) < since) return false
    if (range && (r.totalCents < range[0] || r.totalCents > range[1])) return false
    return needle === '' || r.id.toLowerCase().includes(needle) || r.customer.toLowerCase().includes(needle)
  }
}

// listOrders returns a page of the orders of a project that the query keeps, newest first, with the
// numbers over all of them, as ListOrders does. timeZone is the one of the shop, whose days the
// cards draw. The list without filters, sorted by time either way, is cut from the orders by time
// and the numbers come from the sums the writes keep. A filter, a search or the sort by amount goes
// through every order of the project, as in Go.
export function listOrders(projectId: string, timeZone: string, query: ListQuery = noQuery): OrderList {
  const q = cleanQuery(query)
  const l: OrderList = {
    orders: [], filter: { ...q, page: 1 }, matching: 0, pages: 0, statuses: { all: 0, pending: 0, shipped: 0 }, total: 0, revenueCents: 0, pending: 0, shipped: 0, newestAt: '0001-01-01T00:00:00Z', oldestPending: null, carriers: [],
    daily: { revenueCents: [], orders: [], pending: [], shipped: [] },
  }
  const p = ordersOf(projectId)
  if (p.all.length === 0) return l
  const newest = p.all[p.all.length - 1].placedAt
  l.newestAt = rfc3339(newest)
  l.total = p.all.length
  l.revenueCents = p.revenueCents
  l.pending = p.pending.length
  l.shipped = l.total - l.pending
  if (p.pending.length > 0) {
    // Of the orders placed at the same oldest time, the one with the highest number: the first of
    // them in the list, newest first.
    let i = 0
    while (i + 1 < p.pending.length && p.pending[i + 1].placedAt <= p.pending[0].placedAt) i++
    l.oldestPending = orderRow(p.pending[i])
  }
  for (const [name, n] of p.shippedBy) if (n > 0) l.carriers.push(name)
  l.carriers.sort()
  // The days, labelled by their date in the time zone, counted back from the newest order in steps
  // of 24 hours: two steps can land on the same date, and then they share the sums.
  const z = daysIn(p, timeZone)
  for (let i = 0; i < listDays; i++) {
    const d = z.days.get(local(newest - (listDays - 1 - i) * DAY, timeZone).date)
    l.daily.revenueCents.push(d?.revenueCents ?? 0)
    l.daily.orders.push(d?.orders ?? 0)
    l.daily.pending.push(d?.pending ?? 0)
    l.daily.shipped.push(d?.shipped ?? 0)
  }

  if (q.status === '' && q.carrier === '' && q.period === '' && q.amount === '' && q.q === '' && q.sort === 'placedAt') {
    l.statuses = { all: l.total, pending: l.pending, shipped: l.shipped }
    l.matching = l.total
    l.pages = Math.ceil(l.matching / ListPageSize)
    l.filter.page = Math.min(q.page, Math.max(l.pages, 1))
    const from = (l.filter.page - 1) * ListPageSize
    l.orders = q.dir === 'desc' ? newestFirst(p.all, from, ListPageSize, '') : p.all.slice(from, from + ListPageSize).map(orderRow)
    return l
  }

  const keep = keeper(q, newest)
  const matched: OrderRow[] = []
  for (let i = p.all.length - 1; i >= 0; i--) {
    const row = orderRow(p.all[i])
    if (!keep(row)) continue
    l.statuses.all++
    if (row.status === 'pending') l.statuses.pending++
    else l.statuses.shipped++
    if (q.status === '' || q.status === row.status) matched.push(row)
  }
  // The default list is newest first. Orders that tie on the amount stay in that order whichever way
  // the amount goes (Array.sort is stable), and the time ascending is the default list reversed.
  if (q.sort === 'totalCents') matched.sort((a, b) => (q.dir === 'desc' ? b.totalCents - a.totalCents : a.totalCents - b.totalCents))
  else if (q.dir === 'asc') matched.reverse()
  l.matching = matched.length
  l.pages = Math.ceil(l.matching / ListPageSize)
  l.filter.page = Math.min(q.page, Math.max(l.pages, 1))
  const from = (l.filter.page - 1) * ListPageSize
  l.orders = matched.slice(from, from + ListPageSize)
  return l
}

// getOrder returns the order id of a project, or throws NotFound.
export function getOrder(projectId: string, id: string): Order {
  const p = ordersOf(projectId)
  const o = p.byId.get(id)
  if (!o) throw new NotFound(id)
  return view(p, o)
}

// countPending returns how many orders of a project wait to be shipped.
export function countPending(projectId: string): number {
  return ordersOf(projectId).pending.length
}

function view(p: ProjectOrders, o: OrderRecord): Order {
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
    const product = p.products.get(item.slug)!
    order.items.push({
      slug: item.slug, name: item.name, description: item.description, quantity: item.quantity,
      priceCents: item.priceCents, color: item.color, stock: productStock(o.projectId, item.slug), demand: product.waiting, sold: product.sold,
    })
  }
  return order
}

// customerOrders returns the newest SideListSize other orders of the customer of order id in a project, newest first,
// with the number of orders and the cents they spent in all, this one counted.
export function customerOrders(projectId: string, id: string): { others: OrderRow[]; count: number; spentCents: number } {
  const p = ordersOf(projectId)
  const o = p.byId.get(id)
  if (!o) return { others: [], count: 0, spentCents: 0 }
  const c = p.customers.get(o.customer)!
  return { others: newestFirst(c.orders, 0, SideListSize, id), count: c.orders.length, spentCents: c.spentCents }
}

// ordersWith returns up to n other orders of a project that hold the product slug, newest first. An
// n of 0 or less gives none.
export function ordersWith(projectId: string, slug: string, exceptId: string, n: number): OrderRow[] {
  const product = ordersOf(projectId).products.get(slug)
  return product ? newestFirst(product.orders, 0, n, exceptId) : []
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
  const found = findOrder(projectId, id)
  if (!found) throw new NotFound(id)
  const [p, o] = found
  if (o.shipment) throw new Locked(id)
  rename(p, o, customer.trim())
  o.phone = normalPhone(phone)
  o.address = address.trim()
  return view(p, o)
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
  const found = findOrder(projectId, id)
  if (!found) throw new NotFound(id)
  const [p, o] = found
  if (o.shipment) throw new Locked(id)
  ship(p, o, {
    carrier, trackingNo: tracking.trim(), shippedAt: at,
    events: [{ at, place: warehouse, note: 'Courier collected the parcel' }],
  })
  return view(p, o)
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
  const found = findOrder(projectId, id)
  if (!found) throw new NotFound(id)
  const [p, o] = found
  ;(o.notes ??= []).push({ author: 'Owner', text: text.trim(), at })
  return view(p, o)
}

export interface NewOrder {
  customer: string
  phone: string
  email: string
  address: string
  note: string
  items: NewLine[]
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

// postOrder is the POST of orders/route.go: it checks the order as Validate does, then places it.
export function postOrder(projectId: string, input: NewOrder, at: number): Order {
  const fields = checkNewOrder(input)
  if (fields.length > 0) throw new Invalid(fields)
  return createOrder(projectId, input, at)
}

// createOrder places an order in a project at at, as CreateOrder does: its number is the one after
// the highest of the project, and its lines come from the products the project sells. The caller
// checks the order first. It returns the new order.
export function createOrder(projectId: string, input: NewOrder, at: number): Order {
  const s = shops[projectId]
  if (!s) throw new NotFound(projectId)
  return keep(projectId, s, input, input.items.map(s.line), at)
}

// createStoreOrder places an order of the storefront in the studio shop with the lines its checkout
// bought, as CreateStoreOrder does: they keep the names and prices the checkout totalled.
export function createStoreOrder(input: NewOrder, lines: ProductLine[], at: number): Order {
  return keep(storefrontProject, shops[storefrontProject], input, lines.map(storefrontItem), at)
}

// keep keeps a new order of the shop s of a project with its lines, as keep does in Go.
function keep(projectId: string, s: (typeof shops)[string], input: NewOrder, items: ItemRecord[], at: number): Order {
  const last = orders.get(projectId)?.last ?? 0
  const o: OrderRecord = {
    id: `${s.prefix}-${String(last + 1).padStart(s.width, '0')}`, projectId, customer: input.customer.trim(),
    phone: normalPhone(input.phone), email: input.email.trim(), address: input.address.trim(), note: input.note.trim(),
    placedAt: at, shipment: null, items, notes: null,
  }
  insert(orders, o)
  return view(orders.get(projectId)!, o)
}
