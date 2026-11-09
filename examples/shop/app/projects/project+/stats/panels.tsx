import { Link } from '@tanstack/react-router'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from 'cn'
import { orderTime, percent, yuan } from '../../../format'
import { photo } from '../../../store/images'
import { layout as projectRoute } from '../route.gen'
import { OrderStatus } from '../order-status'
import { Spark } from '../summary-card'
import { page } from './route.gen'

const title = 'font-heading text-lg font-medium tracking-[0.04em]'
const head = 'text-xs font-normal text-muted-foreground'

// ProductTable lists every product the shop sold, with how its pieces went day by day.
export function ProductTable() {
  const { products } = page.useLoaderData()
  const list = products
  const revenue = list.reduce((n, p) => n + p.revenueCents, 0)
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Product sales</CardTitle>
        <CardDescription>Last 30 days: how many of each product sold, with the daily trend on the right.</CardDescription>
        <CardAction className="font-mono text-xs text-muted-foreground">{list.length} products</CardAction>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className={cn(head, 'w-8')}>#</TableHead>
              <TableHead className={head}>Product</TableHead>
              <TableHead className={cn(head, 'text-right')}>Pieces</TableHead>
              <TableHead className={cn(head, 'text-right')}>Orders</TableHead>
              <TableHead className={cn(head, 'text-right')}>Sales</TableHead>
              <TableHead className={cn(head, 'w-32')}>Share</TableHead>
              <TableHead className={cn(head, 'w-28')}>Trend</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.map((p, i) => (
              <TableRow key={p.name}>
                <TableCell className="font-mono text-xs text-muted-foreground">{String(i + 1).padStart(2, '0')}</TableCell>
                <TableCell>
                  <div className="flex items-center gap-3">
                    <img src={photo(p.photo, 'main', 160)} alt="" width={32} height={32} loading="lazy" decoding="async" className="size-8 bg-[#d3dbe2] object-cover dark:bg-[#273240]" />
                    <span className="text-sm">{p.name}</span>
                  </div>
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">{p.quantity}</TableCell>
                <TableCell className="text-right font-mono tabular-nums">{p.orders}</TableCell>
                <TableCell className="text-right font-mono tabular-nums">{yuan(p.revenueCents)}</TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <span className="h-1.5 flex-1 bg-muted">
                      <span className="block h-full bg-primary/70" style={{ width: `${revenue ? Math.round((p.revenueCents / revenue) * 100 * 3) : 0}%`, maxWidth: '100%' }} />
                    </span>
                    <span className="w-10 text-right font-mono text-[11px] text-muted-foreground tabular-nums">{percent(revenue ? p.revenueCents / revenue : 0)}</span>
                  </div>
                </TableCell>
                <TableCell>
                  <Spark points={p.daily} className="h-6 w-24 text-primary" />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

const weekdays = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

// Heatmap shows when orders are placed: the day of the week against two-hour blocks.
export function Heatmap() {
  const { heat } = page.useLoaderData()
  const rows = heat.map((r) => r)
  const peak = Math.max(1, ...rows.flat())
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Order times</CardTitle>
        <CardDescription>The darker the color, the more orders in that time slot.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid grid-cols-[2.5rem_repeat(12,minmax(0,1fr))] gap-1 text-[11px] text-muted-foreground" role="table" aria-label="Order times">
          <span />
          {Array.from({ length: 12 }, (_, b) => (
            <span key={b} className="text-center font-mono">
              {b * 2}
            </span>
          ))}
          {rows.map((row, d) => (
            <div key={weekdays[d]} className="contents" role="row">
              <span className="flex items-center">{weekdays[d]}</span>
              {row.map((n, b) =>
                n > 0 ? (
                  <Tooltip key={b}>
                    <TooltipTrigger asChild>
                      <span role="cell" className="h-6 bg-primary" style={{ opacity: 0.2 + (n / peak) * 0.8 }} />
                    </TooltipTrigger>
                    <TooltipContent>
                      {weekdays[d]} {b * 2}:00 to {b * 2 + 2}:00, {n} orders
                    </TooltipContent>
                  </Tooltip>
                ) : (
                  <span key={b} role="cell" className="h-6 bg-muted" />
                ),
              )}
            </div>
          ))}
        </div>
        <p className="flex items-center gap-2 text-[11px] text-muted-foreground">
          Fewer
          {[0.2, 0.4, 0.6, 0.8, 1].map((o) => (
            <span key={o} className="size-3 bg-primary" style={{ opacity: o }} />
          ))}
          More, up to {peak} orders in a cell
        </p>
      </CardContent>
    </Card>
  )
}

// Carriers is how each carrier did for the shop.
export function Carriers() {
  const { carriers } = page.useLoaderData()
  const list = carriers
  const all = list.reduce((n, c) => n + c.orders, 0)
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Carrier performance</CardTitle>
        <CardDescription>Shipped orders by carrier, with average times.</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className={head}>Carrier</TableHead>
              <TableHead className={cn(head, 'text-right')}>Orders</TableHead>
              <TableHead className={cn(head, 'w-24')}>Share</TableHead>
              <TableHead className={cn(head, 'text-right')}>In the shop</TableHead>
              <TableHead className={cn(head, 'text-right')}>In transit</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.map((c) => (
              <TableRow key={c.code}>
                <TableCell className="text-sm">{c.name}</TableCell>
                <TableCell className="text-right font-mono tabular-nums">{c.orders}</TableCell>
                <TableCell>
                  <span className="block h-1.5 bg-muted">
                    <span className="block h-full bg-primary/70" style={{ width: `${all ? Math.round((c.orders / all) * 100) : 0}%` }} />
                  </span>
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">{c.orders ? `${c.avgDispatchHours.toFixed(1)} hours` : 'None'}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">{c.avgTransitHours ? `${c.avgTransitHours.toFixed(0)} hours` : 'None'}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

// Dispatch is how long the shop took to hand an order to the carrier.
export function Dispatch() {
  const { dispatch, totals } = page.useLoaderData()
  const list = dispatch
  const all = list.reduce((n, b) => n + b.orders, 0)
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>How fast we ship</CardTitle>
        <CardDescription>From order to handing it to the carrier, {totals.avgDispatchHours.toFixed(1)} hours on average.</CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="space-y-3">
          {list.map((b) => (
            <li key={b.label} className="grid grid-cols-[6.5rem_1fr_3.5rem] items-center gap-3 text-sm">
              <span>{b.label}</span>
              <span className="h-1.5 bg-muted">
                <span className="block h-full bg-primary" style={{ width: `${all ? Math.round((b.orders / all) * 100) : 0}%` }} />
              </span>
              <span className="text-right font-mono text-xs text-muted-foreground tabular-nums">
                {b.orders} orders
              </span>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

// Customers are the ones who spent the most.
export function Customers() {
  const { customers, totals } = page.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Regulars</CardTitle>
        <CardDescription>
          {totals.customers} customers, {totals.repeat} bought more than once.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ol className="divide-y">
          {customers.map((c, i) => (
            <li key={c.name} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
              <span className="w-5 font-mono text-xs text-muted-foreground">{String(i + 1).padStart(2, '0')}</span>
              <Avatar className="size-8 rounded-none">
                <AvatarFallback className="rounded-none bg-secondary text-xs text-primary">{c.initials}</AvatarFallback>
              </Avatar>
              <span className="flex-1 text-sm">{c.name}</span>
              <span className="text-right font-mono text-xs tabular-nums">
                {c.orders} orders
                <br />
                <span className="text-muted-foreground">{yuan(c.spentCents)}</span>
              </span>
            </li>
          ))}
        </ol>
      </CardContent>
    </Card>
  )
}

// Recent are the newest orders, with the ones waiting to ship first among them.
export function Recent() {
  const { recent } = page.useLoaderData()
  const { timeZone } = projectRoute.useLoaderData()
  return (
    <Card>
      <CardHeader>
        <CardTitle className={title}>Recent orders</CardTitle>
        <CardAction className="text-xs">
          <Link to="/projects/$project" params={{ project: 'studio' }} className="text-muted-foreground hover:text-foreground">
            All orders
          </Link>
        </CardAction>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className={head}>Order</TableHead>
              <TableHead className={head}>Customer</TableHead>
              <TableHead className={head}>Product</TableHead>
              <TableHead className={head}>Status</TableHead>
              <TableHead className={head}>Placed</TableHead>
              <TableHead className={cn(head, 'text-right')}>Amount</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {recent.map((o) => (
              <TableRow key={o.id}>
                <TableCell>
                  <ProjectOrderLink id={o.id} />
                </TableCell>
                <TableCell className="text-sm">{o.customer}</TableCell>
                <TableCell className="max-w-40 truncate text-sm text-muted-foreground">
                  {o.product}
                  {o.lines > 1 && ` and ${o.lines - 1} more`}
                </TableCell>
                <TableCell>
                  <OrderStatus status={o.status} />
                </TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  <time dateTime={o.placedAt}>{orderTime(o.placedAt, timeZone)}</time>
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">{yuan(o.totalCents)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function ProjectOrderLink({ id }: { id: string }) {
  const project = projectRoute.useLoaderData()
  return (
    <Link to="/projects/$project/orders/$order" params={{ project: project.id, order: id }} className="font-mono text-sm hover:underline">
      {id}
    </Link>
  )
}
