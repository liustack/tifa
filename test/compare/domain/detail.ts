// What the order page shows beside the order itself, ported from
// examples/shop/app/projects/project+/orders/detail.go: how it was paid, what happened to it, and
// where the warehouse picks each line.
import { carrierName } from './shipping.ts'
import { itemCount, total } from './records.ts'
import type { OrderRecord } from './records.ts'
import { MINUTE, SECOND, rfc3339 } from './time.ts'

export interface Payment {
  method: string
  transactionNo: string
  paidAt: string
  subtotalCents: number
  shippingCents: number
  discountCents: number
  paidCents: number
}

export interface LogEntry {
  at: string
  actor: string
  action: string
  detail: string
}

export interface PackLine {
  name: string
  bin: string
  quantity: number
}

const payMethods = ['WeChat Pay', 'Alipay', 'Bank card']

const encoder = new TextEncoder()

// hashOf is FNV-1a over the bytes of the text, 32 bits, as Go's hash/fnv.New32a.
function hashOf(s: string): number {
  let h = 0x811c9dc5
  for (const b of encoder.encode(s)) {
    h ^= b
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return h >>> 0
}

const digits = (n: number, width: number) => String(n).padStart(width, '0')

// payment writes how an order was paid: the method and the transaction number follow from the
// order number, so the same order always shows the same ones.
export function payment(o: OrderRecord): Payment {
  const h = hashOf(o.id)
  const sum = total(o)
  return {
    method: payMethods[h % payMethods.length],
    transactionNo: `4200${digits(h, 10)}${digits(hashOf(o.id + '/pay'), 10)}`,
    paidAt: rfc3339(o.placedAt + 2 * MINUTE),
    subtotalCents: sum, shippingCents: 0, discountCents: 0, paidCents: sum,
  }
}

// packing returns the packing list of an order. A product sits in the same bin whatever the order.
export function packing(o: OrderRecord): PackLine[] {
  const lines: PackLine[] = []
  for (const item of o.items) {
    const h = hashOf(item.slug)
    lines.push({
      name: item.name, quantity: item.quantity,
      bin: `${String.fromCharCode(65 + (h % 4))}-${digits(1 + (Math.floor(h / 4) % 12), 2)}-${1 + (Math.floor(h / 64) % 4)}`,
    })
  }
  return lines
}

// logOf tells what happened to an order, oldest first. The steps before shipping follow from when
// the order was placed, and the ones after from its shipment.
export function logOf(o: OrderRecord): LogEntry[] {
  const p = payment(o)
  const entry = (at: number, actor: string, action: string, detail: string) => ({ at, actor, action, detail })
  const log = [
    entry(o.placedAt, 'Customer', 'Order placed', o.customer + ' placed the order'),
    // The payment is written as text already, so its instant is placed again here.
    entry(o.placedAt + 2 * MINUTE, 'System', 'Payment received', p.method + ', transaction ' + p.transactionNo),
    entry(o.placedAt + 3 * MINUTE, 'System', 'Risk check passed', 'The delivery address and phone number match'),
    entry(o.placedAt + 4 * MINUTE, 'System', 'Ready to ship', 'Added to the Hangzhou warehouse picking queue'),
    entry(o.placedAt + 4 * MINUTE + 20 * SECOND, 'System', 'Stock reserved', `${itemCount(o)} pieces taken out of sellable stock`),
    entry(o.placedAt + 6 * MINUTE, 'System', 'Confirmation sent', 'Order confirmation SMS and email sent to ' + o.customer),
  ]
  const s = o.shipment
  if (s) {
    const carrier = carrierName(s.carrier)
    log.push(
      entry(s.shippedAt - 95 * MINUTE, 'Warehouse', 'Picking started', `${itemCount(o)} pieces, ${o.items.length} bins`),
      entry(s.shippedAt - 50 * MINUTE, 'Warehouse', 'Packing done', 'Boxed, weighed, waybill attached'),
      entry(s.shippedAt - 20 * MINUTE, 'Warehouse', 'Label printed', carrier + ' e-waybill'),
      entry(s.shippedAt, 'Staff', 'Shipped', carrier + ', tracking number ' + s.trackingNo),
      entry(s.shippedAt + MINUTE, 'System', 'Notification sent', 'Shipping SMS and email sent to ' + o.customer),
    )
    const n = s.events.length
    if (n > 0 && s.events[n - 1].note.startsWith('Delivered')) {
      log.push(
        entry(s.events[0].at, 'Courier', 'Picked up', carrier + ' collected the parcel from the warehouse'),
        entry(s.events[n - 1].at, 'Courier', 'Delivered', s.events[n - 1].note),
      )
    }
  }
  for (const n of o.notes ?? []) log.push(entry(n.at, n.author, 'Note added', n.text))
  log.sort((a, b) => a.at - b.at)
  return log.map((e) => ({ at: rfc3339(e.at), actor: e.actor, action: e.action, detail: e.detail }))
}
