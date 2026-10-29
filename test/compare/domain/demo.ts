// The demonstration orders, ported from examples/shop/app/projects/project+/orders/demo.go: 42 for
// studio (SH-1001 to SH-1042) and 12 for weekend (WK-0190 to WK-0201), placed between 2026-09-14
// and 2026-10-13. The newest ones are written out, the older ones follow fixed rules from the
// catalogues and customers below, so the data is the same at every start. This is the seed:
// the Go code makes it at its start, and this module makes it at the start of the Node server,
// the same way. The orders module computes everything else from it for each request.
//
// The seed is ported, not exported from Go, so that both sides start the way a real app does:
// from code that builds its data in memory. The domain check compares what the two compute from
// it, which catches a difference in the seed too.
import { at, DAY, HOUR } from './time.ts'
import { carriers } from './shipping.ts'
import type { ItemRecord, NoteRecord, OrderRecord } from './records.ts'

interface Customer {
  name: string
  phone: string
  email: string
  address: string
}

const customers: Customer[] = [
  { name: '林夏', phone: '13800138000', email: 'linxia@example.com', address: '上海市徐汇区 · 演示地址' },
  { name: '陈默', phone: '13900139001', email: 'chenmo@example.com', address: '杭州市西湖区 · 演示地址' },
  { name: '周宁', phone: '13700137002', email: 'zhouning@example.com', address: '北京市朝阳区 · 演示地址' },
  { name: '许知', phone: '13600136003', email: 'xuzhi@example.com', address: '成都市武侯区 · 演示地址' },
  { name: '苏禾', phone: '13500135004', email: 'suhe@example.com', address: '苏州市姑苏区 · 演示地址' },
  { name: '何安', phone: '18800188005', email: 'hean@example.com', address: '广州市天河区 · 演示地址' },
  { name: '沈星', phone: '18600186006', email: 'shenxing@example.com', address: '南京市鼓楼区 · 演示地址' },
  { name: '江屿', phone: '15900159007', email: 'jiangyu@example.com', address: '厦门市思明区 · 演示地址' },
  { name: '叶青', phone: '15800158008', email: 'yeqing@example.com', address: '武汉市武昌区 · 演示地址' },
  { name: '顾言', phone: '15000150009', email: 'guyan@example.com', address: '西安市雁塔区 · 演示地址' },
  { name: '宋知秋', phone: '17700177010', email: 'songzhiqiu@example.com', address: '长沙市岳麓区 · 演示地址' },
  { name: '唐小满', phone: '18900189011', email: 'tangxiaoman@example.com', address: '重庆市渝中区 · 演示地址' },
  { name: '白露', phone: '13300133012', email: 'bailu@example.com', address: '青岛市市南区 · 演示地址' },
  { name: '秦朗', phone: '15100151013', email: 'qinlang@example.com', address: '天津市和平区 · 演示地址' },
  { name: '温然', phone: '18100181014', email: 'wenran@example.com', address: '昆明市五华区 · 演示地址' },
  { name: '陆川', phone: '19900199015', email: 'luchuan@example.com', address: '深圳市南山区 · 演示地址' },
]

function product(slug: string, name: string, description: string, priceCents: number, color: string, stock: number): ItemRecord {
  return { slug, name, description, quantity: 0, priceCents, color, stock }
}

const studioCatalogue: ItemRecord[] = [
  product('pour-over-kettle', '晨间手冲壶', '雾白 / 600 ml', 32800, 'sand', 23),
  product('linen-coasters', '亚麻杯垫', '原色 / 两件装', 10000, 'olive', 80),
  product('ceramic-cup', '手作陶瓷杯', '青苔绿 / 280 ml', 13400, 'olive', 58),
  product('oak-tray', '原木托盘', '白橡木 / 大号', 58000, 'sand', 6),
  product('reading-candle', '阅读香薰', '雪松与纸张 / 180 g', 19600, 'clay', 40),
  product('brass-bookmarks', '黄铜书签', '哑光 / 两枚装', 6800, 'sand', 200),
  product('stoneware-vase', '粗陶花器', '灰白 / 中号', 24800, 'clay', 11),
  product('linen-apron', '棉麻围裙', '燕麦色 / 均码', 16800, 'olive', 34),
]

const weekendCatalogue: ItemRecord[] = [
  product('canvas-tote', '周末帆布袋', '深蓝 / 标准款', 15800, 'blue', 45),
  product('folding-stool', '露营折叠凳', '军绿 / 承重 120 kg', 13900, 'olive', 52),
  product('picnic-blanket', '野餐垫', '格纹 / 2 × 2 m', 8900, 'blue', 66),
  product('insulated-bottle', '保温水壶', '雾蓝 / 750 ml', 12800, 'blue', 75),
  product('postcards', '手绘明信片', '城市系列 / 6 张', 3600, 'clay', 260),
]

// notes are what some customers write at checkout.
const notes = [
  '送朋友的生日礼物，请不要放价签。',
  '工作日白天家里没人，请放快递柜。',
  '能不能帮忙附一张手写卡片，写「生日快乐」就好。',
  '包装请结实一些，上次的杯子路上磕了一个小口。',
]

export function demoOrders(): OrderRecord[] {
  const list: OrderRecord[] = [
    staffNoted(noted(order('SH-1042', 'studio', 0, at(10, 13, 14, 32), item(studioCatalogue[0], 1), item(studioCatalogue[1], 1)), notes[0]),
      { author: '小满', text: '顾客要手写卡片，已通知包装的同事。', at: at(10, 13, 15, 10) },
      { author: '阿远', text: '手冲壶库存 23 件，这单发完后提醒工坊补货。', at: at(10, 13, 16, 2) }),
    staffNoted(order('SH-1041', 'studio', 1, at(10, 12, 16, 47), item(studioCatalogue[2], 2)),
      { author: '小满', text: '两只杯子分开包，避免路上相碰。', at: at(10, 12, 17, 20) }),
    shipped(order('SH-1040', 'studio', 2, at(10, 12, 10, 8), item(studioCatalogue[3], 1)), 'SF', 'SF1408273615', 6),
    shipped(order('SH-1039', 'studio', 3, at(10, 11, 20, 15), item(studioCatalogue[4], 1)), 'JD', 'JDV004192837465', 14),
    order('WK-0201', 'weekend', 4, at(10, 13, 9, 26), item(weekendCatalogue[0], 1)),
  ]
  // SH-1001 to SH-1038, from 2026-09-14 to 2026-10-11.
  list.push(...generated((n) => `SH-${n}`, 1001, 'studio', 38, 0, 27, 7, 0, studioCatalogue))
  // WK-0190 to WK-0200, from 2026-09-15 to 2026-10-12.
  list.push(...generated((n) => `WK-${String(n).padStart(4, '0')}`, 190, 'weekend', 11, 1, 28, 5, 3, weekendCatalogue))
  return list
}

// generated returns count older orders of a shop, all shipped, placed from firstDay to lastDay
// days after 2026-09-14. The n-th one goes to customer n*step+offset. Their numbers, starting at
// first, follow the time they were placed.
function generated(name: (n: number) => string, first: number, projectId: string, count: number, firstDay: number, lastDay: number, step: number, offset: number, catalogue: ItemRecord[]): OrderRecord[] {
  const list: OrderRecord[] = []
  for (let n = 1; n <= count; n++) {
    const day = firstDay + Math.floor(((n - 1) * (lastDay - firstDay)) / (count - 1))
    const placed = at(9, 14, 9 + ((n * 7) % 12), (n * 17) % 60) + day * DAY
    const prod = (n * 3) % catalogue.length
    const items = [item(catalogue[prod], 1 + Math.floor((n % 5) / 4))]
    if (n % 4 === 0) {
      // Another product: a step of 1 to 3 never comes back round in a catalogue of 5 or more.
      items.push(item(catalogue[(prod + 1 + (n % 3)) % catalogue.length], 1))
    }
    const o = order('', projectId, (n * step + offset) % customers.length, placed, ...items)
    if (n % 6 === 0) o.note = notes[Math.floor(n / 6) % notes.length]
    const carrier = carriers[n % carriers.length].code
    list.push(shipped(o, carrier, `${carrier}${String(7310000000 + n * 7919).padStart(10, '0')}`, 3 + ((n * 5) % 20)))
  }
  list.sort((a, b) => a.placedAt - b.placedAt)
  list.forEach((o, i) => {
    o.id = name(first + i)
  })
  return list
}

function order(id: string, projectId: string, buyer: number, placed: number, ...items: ItemRecord[]): OrderRecord {
  const c = customers[buyer]
  return {
    id, projectId, customer: c.name, phone: c.phone, email: c.email, address: c.address, note: '',
    placedAt: placed, shipment: null, items, notes: null,
  }
}

// shipped returns o shipped with carrier under tracking, hours after it was placed, and delivered
// a day and a half later.
function shipped(o: OrderRecord, carrier: string, tracking: string, hours: number): OrderRecord {
  const left = o.placedAt + hours * HOUR
  o.shipment = { carrier, trackingNo: tracking, shippedAt: left, events: journey(left, o.address) }
  return o
}

// warehouse is where the shops pack and hand their parcels to the carrier.
const warehouse = '杭州市余杭区 · 山海仓'

// journey is the way of a parcel from the warehouse to address, leaving at left.
function journey(left: number, address: string) {
  const trimmed = address.endsWith(' · 演示地址') ? address.slice(0, -' · 演示地址'.length) : address
  const cut = trimmed.indexOf('市')
  let city = cut < 0 ? trimmed : trimmed.slice(0, cut)
  const district = cut < 0 ? '' : trimmed.slice(cut + 1)
  city += '市'
  const steps: [number, string, string][] = [
    [0, warehouse, '快递员已揽收'],
    [3, '杭州转运中心', '已到达'],
    [4, '杭州转运中心', '正在分拣'],
    [5, '杭州转运中心', '已发出，下一站 ' + city + '转运中心'],
    [12, '干线运输途中', '已离开杭州，运输中'],
    [19, city + '转运中心', '已到达'],
    [20, city + '转运中心', '正在分拣'],
    [22, city + '转运中心', '已发出，下一站 ' + district + '营业部'],
    [27, city + district + '营业部', '已到达'],
    [29, city + district + '营业部', '快递员正在派送'],
    [33, city + district, '已签收，签收人：本人'],
  ]
  return steps.map(([h, place, note]) => ({ at: left + h * HOUR, place, note }))
}

// staffNoted returns o with notes staff wrote on it.
function staffNoted(o: OrderRecord, ...staff: NoteRecord[]): OrderRecord {
  o.notes = staff
  return o
}

function noted(o: OrderRecord, note: string): OrderRecord {
  o.note = note
  return o
}

function item(prod: ItemRecord, quantity: number): ItemRecord {
  return { ...prod, quantity }
}

