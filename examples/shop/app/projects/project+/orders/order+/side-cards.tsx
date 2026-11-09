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
import { cn } from 'cn'
import { orderTime, yuan } from '../../../../format'
import { layout as project } from '../../route.gen'
import { page } from './route.gen'

const title = 'font-heading text-lg font-medium tracking-[0.04em]'

// Actions are what the owner does with an order apart from shipping it.
export function Actions() {
  const { order } = page.useLoaderData()
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
            {copied === 'id' ? 'Copied' : 'Copy order number'}
          </Button>
        </TooltipTrigger>
        <TooltipContent>Copy {order.id}</TooltipContent>
      </Tooltip>
      <Button variant="outline" size="sm" onClick={() => window.print()}>
        <Printer />
        Print order
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm">
            More
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          <DropdownMenuLabel>Copy to clipboard</DropdownMenuLabel>
          <DropdownMenuItem onSelect={() => copy('address', `${order.customer} ${order.phone} ${order.address}`)}>Delivery details</DropdownMenuItem>
          <DropdownMenuItem onSelect={() => copy('email', order.email)}>Customer email</DropdownMenuItem>
          {order.shipment && <DropdownMenuItem onSelect={() => copy('tracking', order.shipment!.trackingNo)}>Tracking number</DropdownMenuItem>}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => copy('pay', order.payment.transactionNo)}>Transaction number</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

// Packing is the packing list the warehouse picks from, with a box to tick each line.
export function Packing() {
  const { order } = page.useLoaderData()
  const lines = order.packing
  const [picked, setPicked] = useState<Record<string, boolean>>({})
  const done = lines.filter((l) => picked[l.name]).length
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Picking list</CardTitle>
        <CardDescription>Hangzhou warehouse, picked from the nearest bin to the farthest.</CardDescription>
        <CardAction className="font-mono text-xs text-muted-foreground">
          Picked {done} / {lines.length}
        </CardAction>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-10">
                <span className="sr-only">Picked</span>
              </TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">Product</TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">Bin</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">Quantity</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[...lines]
              .sort((a, b) => a.bin.localeCompare(b.bin))
              .map((l) => (
                <TableRow key={l.name}>
                  <TableCell>
                    <Checkbox aria-label={`Picked ${l.name}`} checked={!!picked[l.name]} onCheckedChange={(on) => setPicked({ ...picked, [l.name]: on === true })} />
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
  const { order, count, spentCents } = page.useLoaderData()
  const stats = [
    ['Orders', `${count}`],
    ['Total spent', yuan(spentCents)],
    ['Per order', yuan(Math.round(spentCents / Math.max(count, 1)))],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Customer</CardTitle>
        <CardAction>
          <Badge variant="outline" className="rounded-none">
            {count > 1 ? (
              <>
                <Repeat2 />
                Repeat customer
              </>
            ) : (
              'New customer'
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
        <p className="text-xs text-muted-foreground">All of {order.customer}'s orders are in this shop, with no refunds.</p>
      </CardContent>
    </Card>
  )
}

// PaymentCard is how the order was paid, and the invoice that goes with it.
export function PaymentCard() {
  const { order } = page.useLoaderData()
  const { timeZone } = project.useLoaderData()
  const pay = order.payment
  const rows: [string, React.ReactNode][] = [
    ['Payment method', pay.method],
    ['Transaction number', <span key="n" className="font-mono text-xs break-all">{pay.transactionNo}</span>],
    ['Paid at', <time key="t" dateTime={pay.paidAt}>{orderTime(pay.paidAt, timeZone)}</time>],
    ['Amount paid', <span key="a" className="font-mono">{yuan(pay.paidCents)}</span>],
  ]
  const invoice: [string, string][] = [
    ['Type', 'E-invoice, personal'],
    ['Title', order.customer],
    ['Status', order.shipment ? 'Issued, sent to the customer email' : 'Issued after shipping'],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle className={cn(title, 'flex items-center gap-2')}>
          <CreditCard className="size-4 text-primary" />
          Payment and invoice
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
            Invoice
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
  const { order } = page.useLoaderData()
  const [place, detail] = order.address.split(' · ')
  const city = place.split('市')[0] + '市'
  const district = place.slice(city.length)
  const checks: [string, string][] = [
    ['City', city],
    ['District', district || 'Not filled in'],
    ['Address', detail ?? 'Not filled in'],
    ['Phone', `${order.phone.slice(0, 3)} **** ${order.phone.slice(7)}`],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle className={cn(title, 'flex items-center gap-2')}>
          <MapPinCheck className="size-4 text-primary" />
          Address check
        </CardTitle>
        <CardAction>
          <Badge variant="secondary" className="rounded-none">
            <BadgeCheck />
            Matches
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

// CarrierAdvice sets the carriers side by side before the order ships: what they promise and what
// they cost. How each has done for the shop is on the stats page.
export function CarrierAdvice() {
  const { carriers } = page.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle className={cn(title, "flex items-center gap-2")}>
          <PackageSearch className="size-4 text-primary" />
          Carrier comparison
        </CardTitle>
        <CardDescription>Within the same city, first kilogram.</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="text-xs font-normal text-muted-foreground">Carrier</TableHead>
              <TableHead className="text-xs font-normal text-muted-foreground">Time</TableHead>
              <TableHead className="text-right text-xs font-normal text-muted-foreground">First kg</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {carriers.map((c, i) => (
              <TableRow key={c.code}>
                <TableCell className="text-sm">
                  {c.name}
                  {i === 0 && (
                    <Badge variant="outline" className="ml-2 rounded-none">
                      Usual
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">{c.days}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">{yuan(c.feeCents)}</TableCell>
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
  const { order } = page.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Stock check</CardTitle>
        <CardDescription>How many the orders to ship need in all, and how many are left to sell.</CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="divide-y">
          {order.items.map((item) => {
            // The stock is what is left to sell: the orders to ship have been taken out of it, so the
            // warehouse holds both.
            const shelf = item.stock + item.demand
            return (
              <li key={item.name} className="space-y-2 py-3 first:pt-0 last:pb-0">
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span>{item.name}</span>
                  <Badge variant={item.stock === 0 ? "destructive" : "secondary"} className="rounded-none">
                    {item.stock === 0 ? "Out of stock" : item.stock <= 10 ? "Low stock" : "Enough"}
                  </Badge>
                </div>
                <div className="h-1.5 bg-muted" role="img" aria-label={`${item.demand} to ship, ${shelf} in the warehouse`}>
                  <span className="block h-full bg-primary" style={{ width: `${Math.min(100, Math.round((item.demand / Math.max(shelf, 1)) * 100))}%` }} />
                </div>
                <p className="flex justify-between font-mono text-xs text-muted-foreground">
                  <span>{item.demand} to ship</span>
                  <span>{shelf} in the warehouse, {item.stock} left to sell</span>
                </p>
              </li>
            )
          })}
        </ul>
      </CardContent>
    </Card>
  )
}

// Checklist is what is left to do with the order: before it ships, or after.
export function Checklist() {
  const { order } = page.useLoaderData()
  const shipped = !!order.shipment
  const items = shipped
    ? [
        ['signed', 'Confirm the customer received it', order.log.some((e) => e.action === 'Delivered')],
        ['invoice', 'E-invoice sent to the customer email', true],
        ['review', 'Invite the customer to review three days after delivery', false],
        ['window', 'Settle after the seven-day return window', false],
      ]
    : [
        ['address', 'Check the delivery address and phone', false],
        ['items', 'Check the products and quantities', false],
        ['message', 'Read the buyer message and internal notes', false],
        ['carrier', 'Pick a carrier', false],
        ['label', 'Stick on the label and enter the tracking number', false],
      ]
  const [done, setDone] = useState<Record<string, boolean>>(() => Object.fromEntries(items.map(([id, , on]) => [id, on as boolean])))
  const left = items.filter(([id]) => !done[id as string]).length
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>{shipped ? 'After shipping' : 'Before shipping'}</CardTitle>
        <CardAction className="font-mono text-xs text-muted-foreground">{left} left</CardAction>
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

const tags = ['Rush', 'Gift', 'Fragile', 'Handwritten card', 'Large order', 'Repeat customer', 'Invoice needed', 'Address to confirm'] as const

// Tags are what the staff mark an order with, so the warehouse knows how to treat it. The marks
// start from what the order already says.
export function Tags() {
  const { order, count } = page.useLoaderData()
  const initial = [
    ...(order.note.includes('卡片') ? ['Handwritten card'] : []),
    ...(order.note.includes('礼物') ? ['Gift'] : []),
    ...(order.note.includes('结实') ? ['Fragile'] : []),
    ...(order.totalCents >= 40000 ? ['Large order'] : []),
    ...(count > 1 ? ['Repeat customer'] : []),
  ]
  const [on, setOn] = useState<string[]>(initial)
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Order tags</CardTitle>
        <CardDescription>The warehouse sees them, the customer does not.</CardDescription>
      </CardHeader>
      <CardContent>
        <ToggleGroup type="multiple" value={on} onValueChange={setOn} aria-label="Order tags" className="flex-wrap gap-1.5 border-0 bg-transparent">
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
  const { order } = page.useLoaderData()
  const pieces = order.items.reduce((n, i) => n + i.quantity, 0)
  const price = order.payment.paidCents
  const goods = Math.round(price * 0.45)
  const packing = pieces * 400
  const fee = Math.round(price * 0.006)
  const shipping = order.shipment ? 600 : 0
  const profit = price - goods - packing - fee - shipping
  const rows: [string, string, boolean?][] = [
    ['Received', yuan(price)],
    ['Product cost, estimated at 45% of the price', `−${yuan(goods)}`],
    ['Packing, ¥4.00 each', `−${yuan(packing)}`],
    ['Payment fee 0.6%', `−${yuan(fee)}`],
    ['Shipping', order.shipment ? `−${yuan(shipping)}` : 'Counted after shipping'],
    ['Gross profit', yuan(profit), true],
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Gross profit estimate</CardTitle>
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
