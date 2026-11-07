import { useState } from 'react'
import { ChartNoAxesColumn } from 'lucide-react'
import { clientOnly } from 'tifa/client'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { photo } from '../../../store/images'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AnimatedNumber } from '../../../animated-number'
import { day, orderDate, percent, yuan } from '../../../format'
import { layout as projectRoute, type LayoutData as Project } from '../route.gen'
import { SummaryCard, SummaryRow } from '../summary-card'
import { page } from './route.gen'
import { Carriers, Customers, Dispatch, Heatmap, ProductTable, Recent } from './panels'

// The charts measure their box and draw in the browser, so they stay out of the server render
// and of the scripts every page loads. The server writes a placeholder of the same height.
const chartPlaceholder = <Skeleton className="h-64 w-full" />
const RevenueChart = clientOnly(() => import('./revenue-chart.client'), chartPlaceholder)
const ProductsChart = clientOnly(() => import('./products-chart.client'), chartPlaceholder)

const ranges = { '7': 'Last 7 days', '30': 'Last 30 days' } as const
type Range = keyof typeof ranges

export default function StatsPage() {
  const { days: allDays, topProducts, totals } = page.useLoaderData()
  // The range is page state, not a query parameter: the cards tween between the two ranges
  // instead of the page loading again.
  const [range, setRange] = useState<Range>('30')
  if (!allDays.length) {
    return (
      <Empty className="min-h-[40vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ChartNoAxesColumn />
          </EmptyMedia>
          <EmptyTitle>No sales data yet</EmptyTitle>
          <EmptyDescription>Once the first order arrives, daily sales are drawn here.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  const days = allDays.slice(-Number(range))
  let revenue = 0
  let orders = 0
  let shipped = 0
  let pieces = 0
  for (const d of days) {
    revenue += d.revenueCents
    orders += d.orders
    shipped += d.shipped
    pieces += d.pieces
  }
  const last = allDays[allDays.length - 1].date

  return (
    <section aria-labelledby="stats-title" className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 id="stats-title" className="font-heading text-xl font-medium tracking-[0.04em]">
            Sales stats
          </h2>
          <p className="text-sm text-muted-foreground">Data up to {orderDate(`${last}T00:00:00Z`, 'UTC')}</p>
        </div>
        <Tabs value={range} onValueChange={(value) => setRange(value as Range)}>
          <TabsList className="rounded-none">
            {Object.entries(ranges).map(([value, label]) => (
              <TabsTrigger key={value} value={value} className="rounded-none">
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <SummaryRow>
        <SummaryCard title="Sales" note={ranges[range]} points={days.map((d) => d.revenueCents)}>
          <AnimatedNumber value={revenue} format={yuan} />
        </SummaryCard>
        <SummaryCard title="Orders" note={ranges[range]} points={days.map((d) => d.orders)}>
          <AnimatedNumber value={orders} />
        </SummaryCard>
        <SummaryCard title="Average order" note="Per order">
          <AnimatedNumber value={orders ? revenue / orders : 0} format={yuan} />
        </SummaryCard>
        <SummaryCard title="Shipping rate" note="Share of orders shipped">
          <AnimatedNumber value={orders ? shipped / orders : 0} format={percent} />
        </SummaryCard>
      </SummaryRow>

      <SummaryRow>
        <SummaryCard title="Pieces sold" note={ranges[range]} points={days.map((d) => d.pieces)}>
          <AnimatedNumber value={pieces} />
        </SummaryCard>
        <SummaryCard title="Pieces per order" note="Average per order">
          <AnimatedNumber value={orders ? pieces / orders : 0} format={(n) => n.toFixed(1)} />
        </SummaryCard>
        <SummaryCard title="Amount to ship" note={`${totals.pendingOrders} orders waiting`}>
          <AnimatedNumber value={totals.pendingCents} format={yuan} />
        </SummaryCard>
        <SummaryCard title="Repeat customers" note={`${totals.repeat} / ${totals.customers} customers bought more than once`}>
          <AnimatedNumber value={totals.customers ? totals.repeat / totals.customers : 0} format={percent} />
        </SummaryCard>
      </SummaryRow>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <Card>
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Daily sales</CardTitle>
            <CardDescription>{ranges[range]}, by order day</CardDescription>
          </CardHeader>
          <CardContent>
            <RevenueChart days={days} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Best sellers</CardTitle>
            <CardDescription>Top 5 by sales in the last 30 days</CardDescription>
          </CardHeader>
          <CardContent>
            <ProductsChart products={topProducts} />
          </CardContent>
        </Card>
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-[22rem_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Sales ranking</CardTitle>
            <CardDescription>Last 30 days, by pieces</CardDescription>
          </CardHeader>
          <CardContent>
            <ol className="divide-y">
              {topProducts.map((p, i) => (
                <li key={p.name} className="flex items-center gap-3 py-2.5 first:pt-0">
                  <span className="w-5 font-mono text-xs text-muted-foreground">{String(i + 1).padStart(2, '0')}</span>
                  <img
                    src={photo(p.slug, 'main', 160)}
                    alt=""
                    width={40}
                    height={40}
                    loading="lazy"
                    decoding="async"
                    className="size-10 bg-[#d3dbe2] object-cover dark:bg-[#273240]"
                  />
                  <span className="flex-1 truncate text-sm">{p.name}</span>
                  <span className="text-right font-mono text-xs tabular-nums">
                    {p.quantity} pieces
                    <br />
                    <span className="text-muted-foreground">{yuan(p.revenueCents)}</span>
                  </span>
                </li>
              ))}
            </ol>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="font-heading text-lg font-medium tracking-[0.04em]">Daily detail</CardTitle>
            <CardDescription>{ranges[range]}, days with orders</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs font-normal text-muted-foreground">Date</TableHead>
                  <TableHead className="text-right text-xs font-normal text-muted-foreground">Orders</TableHead>
                  <TableHead className="text-right text-xs font-normal text-muted-foreground">Shipped</TableHead>
                  <TableHead className="text-right text-xs font-normal text-muted-foreground">Sales</TableHead>
                  <TableHead className="w-1/3 text-xs font-normal text-muted-foreground">
                    <span className="sr-only">Share</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {days
                  .filter((d) => d.orders > 0)
                  .reverse()
                  .map((d) => (
                    <TableRow key={d.date}>
                      <TableCell className="font-mono text-xs">{day(d.date)}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{d.orders}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{d.shipped}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{yuan(d.revenueCents)}</TableCell>
                      <TableCell>
                        <span className="block h-1.5 bg-primary/70" style={{ width: `${revenue ? Math.round((d.revenueCents / revenue) * 400) : 0}%` }} />
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>

      <ProductTable />

      <div className="grid items-start gap-4 lg:grid-cols-2">
        <Heatmap />
        <div className="grid gap-4">
          <Carriers />
          <Dispatch />
        </div>
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-[22rem_minmax(0,1fr)]">
        <Customers />
        <Recent />
      </div>
    </section>
  )
}

// The title names the shop, which the project Layout loads.
export const head = ({ matches }: { matches: Array<{ routeId: string; loaderData?: unknown }> }) => {
  const project = matches.find((m) => m.routeId === projectRoute.id)?.loaderData as Project | undefined
  return { meta: project ? [{ title: `Sales stats · ${project.name} · Shanhai Workbench` }] : [] }
}
