// The records the orders are kept as, and what is worked out from one. Times are instants in
// milliseconds. Go keeps time.Time and writes it as RFC 3339 when it makes the JSON, which is what
// the views of orders.ts carry.

export interface NoteRecord {
  author: string
  text: string
  at: number
}

export interface TrackEventRecord {
  at: number
  place: string
  note: string
}

export interface ShipmentRecord {
  carrier: string // a code of carriers
  trackingNo: string
  shippedAt: number
  events: TrackEventRecord[] // oldest first, as the carrier reports them
}

export interface ItemRecord {
  slug: string
  name: string
  description: string
  quantity: number
  priceCents: number
  color: string
  stock: number
}

export interface OrderRecord {
  id: string
  projectId: string
  customer: string
  phone: string
  email: string
  address: string
  note: string
  placedAt: number
  shipment: ShipmentRecord | null // null until the order ships
  items: ItemRecord[]
  notes: NoteRecord[] | null
}

// Status of an order. Pages show their own labels for them.
const statusPending = 'pending'
const statusShipped = 'shipped'

export const initials = (name: string): string => Array.from(name)[0]

export const status = (o: OrderRecord): string => (o.shipment ? statusShipped : statusPending)

export function total(o: OrderRecord): number {
  let sum = 0
  for (const item of o.items) sum += item.priceCents * item.quantity
  return sum
}

export function itemCount(o: OrderRecord): number {
  let n = 0
  for (const item of o.items) n += item.quantity
  return n
}

