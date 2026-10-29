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

const payMethods = ['微信支付', '支付宝', '银行卡']

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
    entry(o.placedAt, '顾客', '下单', o.customer + ' 提交了订单'),
    // The payment is written as text already, so its instant is placed again here.
    entry(o.placedAt + 2 * MINUTE, '系统', '付款成功', p.method + '，交易号 ' + p.transactionNo),
    entry(o.placedAt + 3 * MINUTE, '系统', '风控通过', '收货地址和手机号核验一致'),
    entry(o.placedAt + 4 * MINUTE, '系统', '进入待发货', '已同步到杭州仓的拣货队列'),
    entry(o.placedAt + 4 * MINUTE + 20 * SECOND, '系统', '锁定库存', `${itemCount(o)} 件商品已从可售库存里扣出`),
    entry(o.placedAt + 6 * MINUTE, '系统', '发送确认', '订单确认短信和邮件已发给 ' + o.customer),
  ]
  const s = o.shipment
  if (s) {
    const carrier = carrierName(s.carrier)
    log.push(
      entry(s.shippedAt - 95 * MINUTE, '仓库', '开始拣货', `${itemCount(o)} 件商品，${o.items.length} 个货位`),
      entry(s.shippedAt - 50 * MINUTE, '仓库', '打包完成', '封箱称重，贴好面单'),
      entry(s.shippedAt - 20 * MINUTE, '仓库', '打印面单', carrier + '电子面单'),
      entry(s.shippedAt, '店员', '发货', carrier + '，运单号 ' + s.trackingNo),
      entry(s.shippedAt + MINUTE, '系统', '发送通知', '发货短信和邮件已发给 ' + o.customer),
    )
    const n = s.events.length
    if (n > 0 && s.events[n - 1].note.startsWith('已签收')) {
      log.push(
        entry(s.events[0].at, '快递', '揽收', carrier + '已从仓库取走包裹'),
        entry(s.events[n - 1].at, '快递', '已签收', s.events[n - 1].note),
      )
    }
  }
  for (const n of o.notes ?? []) log.push(entry(n.at, n.author, '添加备注', n.text))
  log.sort((a, b) => a.at - b.at)
  return log.map((e) => ({ at: rfc3339(e.at), actor: e.actor, action: e.action, detail: e.detail }))
}
