import { useState } from 'react'
import { BadgeCheck, Copy, CreditCard, FileText, MapPinCheck, PackageSearch, Printer, Repeat2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { orderTime, yuan } from '../../../../format'
import { route as project } from '../../+layout'
import { route } from './+route'

const title = 'font-heading text-lg font-medium tracking-[0.04em]'

// Actions are what the owner does with an order apart from shipping it.
export function Actions() {
  const { order } = route.useLoaderData()
  const [copied, setCopied] = useState('')
  async function copy(what: string, text: string) {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(what)
    } catch {
      setCopied('')
    }
  }
  return (
    <div className="flex items-center gap-2">
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="outline" size="sm" onClick={() => copy('id', order.id)}>
            <Copy />
            {copied === 'id' ? '已复制' : '复制订单号'}
          </Button>
        </TooltipTrigger>
        <TooltipContent>复制 {order.id}</TooltipContent>
      </Tooltip>
      <Button variant="outline" size="sm" onClick={() => window.print()}>
        <Printer />
        打印订单
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm">
            更多
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          <DropdownMenuLabel>复制到剪贴板</DropdownMenuLabel>
          <DropdownMenuItem onSelect={() => copy('address', `${order.customer} ${order.phone} ${order.address}`)}>收货信息</DropdownMenuItem>
          <DropdownMenuItem onSelect={() => copy('email', order.email)}>顾客邮箱</DropdownMenuItem>
          {order.shipment && <DropdownMenuItem onSelect={() => copy('tracking', order.shipment!.trackingNo)}>运单号</DropdownMenuItem>}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => copy('pay', order.payment.transactionNo)}>交易号</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

// Packing is the packing list the warehouse picks from, with a box to tick each line.
export function Packing() {
  const { order } = route.useLoaderData()
  const lines = order.packing ?? []
  const [picked, setPicked] = useState<Record<string, boolean>>({})
  const done = lines.filter((l) => picked[l.name]).length
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>拣货单</CardTitle>
        <CardDescription>杭州仓，按货位从近到远拣。</CardDescription>
        <CardAction className="font-mono text-xs text-muted-foreground">
          已拣 {done} / {lines.length}
        </CardAction>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-10">
                <span className="sr-only">已拣</span>
              </TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">商品</TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">货位</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">数量</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[...lines]
              .sort((a, b) => a.bin.localeCompare(b.bin))
              .map((l) => (
                <TableRow key={l.name}>
                  <TableCell>
                    <Checkbox aria-label={`已拣 ${l.name}`} checked={!!picked[l.name]} onCheckedChange={(on) => setPicked({ ...picked, [l.name]: on === true })} />
                  </TableCell>
                  <TableCell className={cn(picked[l.name] && 'text-muted-foreground line-through')}>{l.name}</TableCell>
                  <TableCell className="font-mono text-xs">{l.bin}</TableCell>
                  <TableCell className="text-right font-mono tabular-nums">{l.quantity}</TableCell>
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

// Customer is who bought, and how much they have bought here.
export function Customer() {
  const { order, count, spentCents } = route.useLoaderData()
  const stats = [
    ['订单', `${count} 单`],
    ['累计消费', yuan(spentCents)],
    ['单均', yuan(Math.round(spentCents / Math.max(count, 1)))],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>顾客</CardTitle>
        <CardAction>
          <Badge variant="outline" className="rounded-none">
            {count > 1 ? (
              <>
                <Repeat2 />
                回头客
              </>
            ) : (
              '新顾客'
            )}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-4">
        <dl className="grid grid-cols-3 divide-x border text-center">
          {stats.map(([k, v]) => (
            <div key={k} className="flex flex-col gap-0.5 px-2 py-3">
              <dt className="text-xs text-muted-foreground">{k}</dt>
              <dd className="font-mono text-sm tabular-nums">{v}</dd>
            </div>
          ))}
        </dl>
        <p className="text-xs text-muted-foreground">{order.customer} 的订单都在这家店，没有退款记录。</p>
      </CardContent>
    </Card>
  )
}

// PaymentCard is how the order was paid, and the invoice that goes with it.
export function PaymentCard() {
  const { order } = route.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const pay = order.payment
  const rows: [string, React.ReactNode][] = [
    ['支付方式', pay.method],
    ['交易号', <span key="n" className="font-mono text-xs break-all">{pay.transactionNo}</span>],
    ['付款时间', <time key="t" dateTime={pay.paidAt}>{orderTime(pay.paidAt, timeZone)}</time>],
    ['实付金额', <span key="a" className="font-mono">{yuan(pay.paidCents)}</span>],
  ]
  const invoice: [string, string][] = [
    ['类型', '电子发票，个人'],
    ['抬头', order.customer],
    ['状态', order.shipment ? '已开具，发到顾客邮箱' : '发货后开具'],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle className={cn(title, 'flex items-center gap-2')}>
          <CreditCard className="size-4 text-primary" />
          付款与发票
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{k}</dt>
              <dd>{v}</dd>
            </div>
          ))}
        </dl>
        <div className="border-t pt-4">
          <p className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
            <FileText className="size-3.5" />
            发票
          </p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
            {invoice.map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="text-muted-foreground">{k}</dt>
                <dd>{v}</dd>
              </div>
            ))}
          </dl>
        </div>
      </CardContent>
    </Card>
  )
}

// AddressCheck splits the address the way the carrier reads it, and says what was checked.
export function AddressCheck() {
  const { order } = route.useLoaderData()
  const [place, detail] = order.address.split(' · ')
  const city = place.split('市')[0] + '市'
  const district = place.slice(city.length)
  const checks: [string, string][] = [
    ['城市', city],
    ['区县', district || '未填写'],
    ['详细地址', detail ?? '未填写'],
    ['手机号', `${order.phone.slice(0, 3)} **** ${order.phone.slice(7)}`],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle className={cn(title, 'flex items-center gap-2')}>
          <MapPinCheck className="size-4 text-primary" />
          地址核验
        </CardTitle>
        <CardAction>
          <Badge variant="secondary" className="rounded-none">
            <BadgeCheck />
            一致
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent>
        <ul className="divide-y text-sm">
          {checks.map(([k, v]) => (
            <li key={k} className="flex justify-between gap-4 py-2 first:pt-0 last:pb-0">
              <span className="text-muted-foreground">{k}</span>
              <span className="text-right">{v}</span>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

// CarrierAdvice sets the carriers side by side before the order ships: what they promise, what
// they cost, and how fast the shop has handed parcels to each so far.
export function CarrierAdvice() {
  const { carriers, carrierStats } = route.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle className={cn(title, "flex items-center gap-2")}>
          <PackageSearch className="size-4 text-primary" />
          快递对比
        </CardTitle>
        <CardDescription>同城区内，首重 1 公斤。</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="text-xs font-normal text-muted-foreground">快递</TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">时效</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">首重</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">已发</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(carriers ?? []).map((c, i) => (
              <TableRow key={c.code}>
                <TableCell className="text-sm">
                  {c.name}
                  {i === 0 && (
                    <Badge variant="outline" className="ml-2 rounded-none">
                      常用
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">{c.days}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">{yuan(c.feeCents)}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">{(carrierStats ?? []).find((s) => s.code === c.code)?.orders ?? 0} 单</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

// StockCheck tells whether the shelf holds what the waiting orders need of each product in this one.
export function StockCheck() {
  const { order } = route.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>库存检查</CardTitle>
        <CardDescription>待发货的订单一共需要多少，仓里还有多少。</CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="divide-y">
          {(order.items ?? []).map((item) => {
            const left = item.stock - item.demand
            return (
              <li key={item.name} className="space-y-2 py-3 first:pt-0 last:pb-0">
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span>{item.name}</span>
                  <Badge variant={left < 0 ? "destructive" : "secondary"} className="rounded-none">
                    {left < 0 ? "缺货" : left <= 10 ? "库存紧张" : "够发"}
                  </Badge>
                </div>
                <div className="h-1.5 bg-muted" role="img" aria-label={`待发 ${item.demand} 件，库存 ${item.stock} 件`}>
                  <span className="block h-full bg-primary" style={{ width: `${Math.min(100, Math.round((item.demand / Math.max(item.stock, 1)) * 100))}%` }} />
                </div>
                <p className="flex justify-between font-mono text-xs text-muted-foreground">
                  <span>待发 {item.demand} 件</span>
                  <span>库存 {item.stock} 件，发完剩 {left}</span>
                </p>
              </li>
            )
          })}
        </ul>
      </CardContent>
    </Card>
  )
}

// CarrierTimes sets the carrier of this parcel beside the others, by how long parcels took.
export function CarrierTimes() {
  const { order, carrierStats } = route.useLoaderData()
  const list = (carrierStats ?? []).filter((c) => c.orders > 0)
  const slowest = Math.max(...list.map((c) => c.avgDispatchHours + c.avgTransitHours), 1)
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>快递用时对比</CardTitle>
        <CardDescription>这家店所有已发货订单的平均，从下单到送达。</CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="space-y-3">
          {list.map((c) => (
            <li key={c.code} className="space-y-1.5">
              <p className="flex justify-between text-sm">
                <span className={cn(c.code === order.shipment?.carrier && "font-medium text-primary")}>{c.name}</span>
                <span className="font-mono text-xs text-muted-foreground tabular-nums">
                  {Math.round(c.avgDispatchHours + c.avgTransitHours)} 小时 · {c.orders} 单
                </span>
              </p>
              <div className="flex h-1.5 bg-muted" aria-hidden>
                <span className="block h-full bg-chart-4" style={{ width: `${(c.avgDispatchHours / slowest) * 100}%` }} />
                <span className="block h-full bg-primary" style={{ width: `${(c.avgTransitHours / slowest) * 100}%` }} />
              </div>
            </li>
          ))}
        </ul>
        <p className="mt-4 flex gap-4 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="size-2 bg-chart-4" />
            店里发出
          </span>
          <span className="flex items-center gap-1.5">
            <span className="size-2 bg-primary" />
            路上
          </span>
        </p>
      </CardContent>
    </Card>
  )
}

// Checklist is what is left to do with the order: before it ships, or after.
export function Checklist() {
  const { order } = route.useLoaderData()
  const shipped = !!order.shipment
  const items = shipped
    ? [
        ['signed', '确认顾客已签收', (order.log ?? []).some((e) => e.action === '已签收')],
        ['invoice', '电子发票已发到顾客邮箱', true],
        ['review', '签收三天后邀请顾客写评价', false],
        ['window', '七天退换期结束后结算', false],
      ]
    : [
        ['address', '核对收货地址和手机号', false],
        ['items', '核对商品和数量', false],
        ['message', '看一遍买家留言和内部备注', false],
        ['carrier', '选好快递公司', false],
        ['label', '贴好面单，填运单号', false],
      ]
  const [done, setDone] = useState<Record<string, boolean>>(() => Object.fromEntries(items.map(([id, , on]) => [id, on as boolean])))
  const left = items.filter(([id]) => !done[id as string]).length
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>{shipped ? '发货后跟进' : '发货前检查'}</CardTitle>
        <CardAction className="font-mono text-xs text-muted-foreground">还剩 {left} 项</CardAction>
      </CardHeader>
      <CardContent>
        <ul className="flex flex-col gap-2.5">
          {items.map(([id, label]) => (
            <li key={id as string} className="flex items-center gap-2.5 text-sm">
              <Checkbox id={`check-${id}`} checked={!!done[id as string]} onCheckedChange={(on) => setDone({ ...done, [id as string]: on === true })} />
              <label htmlFor={`check-${id}`} className={cn('flex-1', done[id as string] && 'text-muted-foreground line-through')}>
                {label as string}
              </label>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

const tags = ['加急', '礼品', '易碎', '手写卡片', '大额订单', '回头客', '需开票', '地址待确认'] as const

// Tags are what the staff mark an order with, so the warehouse knows how to treat it. The marks
// start from what the order already says.
export function Tags() {
  const { order, count } = route.useLoaderData()
  const initial = [
    ...(order.note.includes('卡片') ? ['手写卡片'] : []),
    ...(order.note.includes('礼物') ? ['礼品'] : []),
    ...(order.note.includes('结实') ? ['易碎'] : []),
    ...(order.totalCents >= 40000 ? ['大额订单'] : []),
    ...(count > 1 ? ['回头客'] : []),
  ]
  const [on, setOn] = useState<string[]>(initial)
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>订单标记</CardTitle>
        <CardDescription>仓库看得到，顾客看不到。</CardDescription>
      </CardHeader>
      <CardContent>
        <ToggleGroup type="multiple" value={on} onValueChange={setOn} aria-label="订单标记" className="flex-wrap gap-1.5 border-0 bg-transparent">
          {tags.map((tag) => (
            <ToggleGroupItem key={tag} value={tag} className="h-7 w-auto border px-2.5 text-xs">
              {tag}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </CardContent>
    </Card>
  )
}

// Margin is what the order earns after what it cost, estimated from the shop's usual costs.
export function Margin() {
  const { order } = route.useLoaderData()
  const pieces = (order.items ?? []).reduce((n, i) => n + i.quantity, 0)
  const price = order.payment.paidCents
  const goods = Math.round(price * 0.45)
  const packing = pieces * 400
  const fee = Math.round(price * 0.006)
  const shipping = order.shipment ? 600 : 0
  const profit = price - goods - packing - fee - shipping
  const rows: [string, string, boolean?][] = [
    ['实收', yuan(price)],
    ['商品成本，按售价 45% 估', `−${yuan(goods)}`],
    ['包装，每件 ¥4.00', `−${yuan(packing)}`],
    ['支付手续费 0.6%', `−${yuan(fee)}`],
    ['运费', order.shipment ? `−${yuan(shipping)}` : '发货后计入'],
    ['毛利', yuan(profit), true],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>毛利估算</CardTitle>
        <CardAction className="font-mono text-xs text-muted-foreground">{Math.round((profit / price) * 100)}%</CardAction>
      </CardHeader>
      <CardContent>
        <dl className="divide-y text-sm">
          {rows.map(([k, v, strong]) => (
            <div key={k} className={cn('flex justify-between gap-4 py-2 first:pt-0 last:pb-0', strong && 'font-medium')}>
              <dt className={strong ? '' : 'text-muted-foreground'}>{k}</dt>
              <dd className="font-mono tabular-nums">{v}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}
